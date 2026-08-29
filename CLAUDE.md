# CLAUDE.md

## What is Mise?

Mise ("meez") is a configuration-as-code CLI tool for restaurant POS platforms. It lets multi-location restaurant operators define, version-control, and deploy POS configuration (menus, tax rates, service charges, discounts) as declarative YAML — with plan/apply workflows, drift detection, and rollback via git.

The name comes from "mise en place" — the kitchen discipline of having everything in its place before service begins.

## Key Documents

- `docs/mise-prd.md` — Full PRD with architecture, data models, CLI spec, Square API integration details, and 12-week milestone plan
- `docs/mise-roadmap.md` — Product roadmap (Phases 0–4) and go-to-market strategy

Read the PRD before making architectural decisions. It contains the provider interface contract, state file schema, config file format, and Square API mapping.

## Architecture

Mise follows Terraform's architecture pattern:

```
CLI (cobra commands) → Core Engine → Provider Interface → POS Adapter (Square, Toast, etc.)
```

- **Core engine** (`internal/engine/`) — platform-agnostic plan/apply/drift logic
- **Output formatting** (`pkg/output/`) — colored plan diffs and reports
- **Provider interface** (`internal/provider/interface.go`) — the contract every POS adapter implements
- **Provider registry** (`internal/provider/registry.go`) — maps platform names to adapter constructors
- **Square adapter** (`internal/providers/square/`) — first POS adapter, uses Square Catalog + Locations APIs
- **Config loader** (`internal/config/`) — parses mise.yaml and resource YAML files
- **State manager** (`internal/state/`) — reads/writes .mise/state.json
- **Credentials store** (`internal/credentials/`) — reads/writes .mise/credentials (0600), provider-agnostic

The engine never calls POS APIs directly. It only talks to the Provider interface. New POS platforms are added by implementing that interface — no engine changes needed.

## Tech Stack

- **Go 1.22+** — core language
- **cobra** — CLI framework (`github.com/spf13/cobra`)
- **viper** — configuration management (`github.com/spf13/viper`)
- **yaml.v3** — YAML parsing (`gopkg.in/yaml.v3`)
- **testify** — test assertions (`github.com/stretchr/testify`)

## Common Commands

```bash
make install      # build with version info and put `mise` on PATH
make build        # Build binary to bin/mise
make run ARGS="plan"  # Run without building
make test         # Run all unit tests (no network, no credentials)
make test-race    # Adds race detection — needs cgo and a C compiler
make test-integration  # Against a real Square sandbox; needs SQUARE_ACCESS_TOKEN
make cover        # Per-package coverage
make lint         # Run golangci-lint
make fmt          # Format all Go files
```

Integration tests live in `cmd/integration_test.go` behind a `//go:build
integration` tag and skip without a sandbox token, so `go test ./...` never
touches the network. They create and delete real catalog objects — sandbox
only, and they refuse to run if `MISE_ALLOW_PRODUCTION` is set.

## Development Conventions

- Config files use YAML. State files use JSON.
- Resources reference each other with `ref(type.name)` syntax. Adapters emit `provider.Ref{ResourceType, ProviderID}` values inside a resource's properties rather than raw IDs or ref strings; the engine rewrites them into `ref(type.name)` once every resource has a config name. This keeps platform-specific knowledge (which fields are references) in the adapter and naming policy in the engine.
- Config names are derived by the engine: the platform's display name, slugified ("GA State Sales Tax" → `ga_state_sales_tax`). Collisions get a numeric suffix assigned in provider-ID order, so names stay stable across runs.
- `mise fetch` must be idempotent — fetching twice with nothing changed produces byte-identical files. That means deterministic ordering everywhere: sorted resources, sorted keys, sorted reference lists.
- The provider interface uses `context.Context` on all methods for cancellation and timeouts.
- Commands exit 0 on success and 1 on failure, except `mise drift`, which exits 2 when drift is found so a scheduled check can tell "drift detected" from "the command broke". An error implementing `cmd.ExitCoder` chooses its own status; one implementing `Silent()` has already reported itself and is not printed again. A run stopped by Ctrl-C exits 130.
- Error handling follows Go conventions: return errors, don't panic. API errors are classified as retryable (429, 5xx) or non-retryable (4xx).
- Square sandbox (`https://connect.squareupsandbox.com/v2`) is used for all development and testing.
- Secrets live only in `.mise/credentials` (0600, gitignored). `mise.yaml` records the auth *method*, never a token. `SQUARE_ACCESS_TOKEN` / `MISE_SQUARE_ACCESS_TOKEN` override the stored file so CI never runs `mise init`.
- Interactive prompts must degrade to a clear error when stdin is not a terminal — never block, never silently take a default.
- Mise never deletes POS resources unless the operator explicitly passes `--destroy`. Safety-first default.
- State files and credentials (`.mise/`) are gitignored and must never be committed.

## Current Status

Phase 0 — Foundation, Milestones 1–6 complete except release packaging. All five commands work end to end against Square.

- `mise init` — OAuth2 authorization-code flow (local callback listener, anti-CSRF state, token exchange and refresh) and personal access tokens, credential verification via `GET /v2/locations`, workspace scaffolding.
- `mise fetch` — reads the live catalog (items, categories, taxes, discounts, modifier lists) plus locations, deduplicates resources across locations, resolves cross-resource references, generates YAML config files, and writes `.mise/state.json`.

- `mise plan` — loads the declared YAML, resolves `${group.*}` and `ref()`, reads live state, and prints a colored diff. Supports `--target`, `--location`, and `--out` for a saved plan.

- `mise apply` — executes a plan in dependency order via Square's batch-upsert, with deterministic idempotency keys, version tokens for optimistic concurrency, a workspace lock, and partial-failure handling that records what landed.
- `mise version` — build version, commit, and platform; `--json` for scripts.

- `mise drift` — compares the last-known state against the live POS and reports what changed outside Mise. Read-only; exits 2 when drift is found so scheduled checks can alert without parsing output. Supports `--location`, `--type`, `--json`.

Milestone 6 delivered the integration suite, hardened error handling, and docs.
**goreleaser / release packaging is deliberately not done yet** — it is the only
remaining Phase 0 item.

Hardening added in Milestone 6:

- **Transient network faults are retried.** A dropped keep-alive or connection reset used to fail a whole apply; `retryableNetworkError` now treats them as retryable, while still refusing to retry a cancelled context or an unresolvable host.
- **`Retry-After` is honored** on 429, in both its seconds and HTTP-date forms, capped at 30s. Backoff otherwise uses equal jitter, so parallel per-location reads that hit the same 429 do not retry in lockstep.
- **API errors carry a `Hint`** — a 401 says to check the environment and re-run `mise init --force` rather than just "UNAUTHORIZED", and error bodies are truncated at 512 bytes so an HTML error page from a proxy cannot bury the output.
- **The rate limiter takes a context.** A fetch across many locations spends most of its time queued behind it, so an uncancellable `Wait` made Ctrl-C useless.
- **Ctrl-C is handled, not fatal** — see the interrupt decision below.

## Known Design Decisions To Revisit

- **Plan compares by provider ID, never by name.** Declared `ref(type.name)` is resolved to a provider ID through state before comparison, and live `provider.Ref` values are normalized to their IDs. Renaming a resource in YAML therefore does not read as a change to everything pointing at it. Drift (Milestone 5) must do the same.
- **Plan canonicalizes values through JSON before comparing.** YAML decodes `450` as `int`, the Square client as `int64`, JSON as `float64`. Without that round-trip every price would show as changed on every plan.
- **`--location` narrows both sides of the diff.** Scoping a plan to one location intersects the desired location set with the scope too; otherwise a resource that also applies elsewhere reads as "gained a location", which is an artifact of the filter.
- **Apply orders changes into dependency waves, not a flat list.** `engine.OrderChanges` groups changes so everything in one wave is independent and can go up in a single batch; references to resources created in an earlier wave are resolved between waves, once those resources have provider IDs. Only dependencies *within the plan* constrain ordering — a reference to something already live imposes none.
- **`provider.Resource` has both `LocationID` and `LocationIDs`.** The singular one is read-side ("I saw this at location X"); the plural is write-side. Square creates one catalog object carrying its own `present_at_location_ids`, not one object per location, so writes need the whole set.
- **`BatchApplier` is an optional provider capability.** Adapters that can write many resources per call implement it; the engine falls back to `Create`/`Update` otherwise, so a simpler adapter stays correct.
- **A failed wave stops the apply.** Anything later depends on what just failed, so continuing would cascade confusing errors. State is still saved for what succeeded — otherwise a re-run would create those resources twice.
- **State's reference shape depends on which command wrote it.** `fetch` records `"category": "ref(square_catalog_category.beverages)"`; `apply` records the resolved provider ID `"CAT_1"`. Drift has to tolerate both, so `engine.normalizeReferences` maps `provider.Ref`, `ref(type.name)` strings, and raw IDs onto one form (the provider ID) before comparing. Worth unifying eventually, but the normalizer makes the difference harmless and there are tests for both shapes. Anything comparing state against live must use it.
- **Drift matches live objects to state entries by provider ID, never by config name.** A rename in YAML is not drift, and a slug collision must never make two resources look like each other.
- **Drift is strictly read-only** — it does not update state. A drift report is evidence of a discrepancy, not permission to accept it; accepting means running `mise fetch`.
- **Drift reports per resource, not per location, unlike the PRD's example.** Square's catalog is account-wide, so a changed tax at forty locations is one object, and grouping by location would print it forty times. Each drift names its affected locations instead. A location-scoped platform like Toast may want the PRD's grouping back.
- **An interrupted write reports "unknown", never "failed".** The first Ctrl-C cancels the context so the deferred lock release and state save still run. But if the signal lands while a batch is in flight, Mise cannot know whether Square applied it — the request may have succeeded with the response lost. `engine.Apply` therefore returns an unknown-outcome error naming `mise drift`, and records nothing in `result.Failed`. Marking those resources failed would send the operator to re-create objects that already exist. Cancellation *between* waves is the clean case and stops with no failures at all.
- **Cancellation is distinguished from failure everywhere it crosses a boundary.** The client returns `ctx.Err()` rather than "request failed"; `readAllLocations` returns `ctx.Err()` rather than "cannot read X at location Y", which would blame a location for the operator's own Ctrl-C; `cmd.ExitCode` maps `context.Canceled` to 130 but leaves `DeadlineExceeded` at 1, because a timeout means Mise gave up and that is a real failure.
- **Integration tests refuse to run against production.** They create and delete catalog objects. They skip without a token, and `MISE_ALLOW_PRODUCTION` being set is a hard failure rather than an opt-in — there is deliberately no way to point them at a live restaurant's menu.

## Milestone Sequence

1. **Skeleton** (done) — Project structure, cobra CLI, provider interface, `mise init` with Square OAuth2 and access token auth
2. **Fetch** (done) — `mise fetch` pulls live config from Square into YAML files and writes the initial state file
3. **Plan** (done) — `mise plan` computes diffs between declared YAML and live state
4. **Apply** (done) — `mise apply` pushes changes to Square via batch API
5. **Drift** (done) — `mise drift` detects changes made outside Mise
6. **Polish** (integration tests, error handling, documentation done; **goreleaser outstanding**)

## Square API Notes

- Base URL: `https://connect.squareup.com/v2` (prod), `https://connect.squareupsandbox.com/v2` (sandbox)
- Auth: Bearer token in Authorization header
- Rate limit: ~40 req/s. The client has a built-in token-bucket limiter set to 30/s.
- Catalog objects use `present_at_location_ids` for location scoping
- Batch upsert (`POST /v2/catalog/batch-upsert`) handles up to 10,000 objects — prefer this over individual calls during apply
- OAuth endpoints live at the host root (`/oauth2/authorize`, `/oauth2/token`), not under `/v2`. The token endpoint takes a JSON body and returns an RFC3339 `expires_at`, not `expires_in` seconds — which is why Mise does its own token exchange instead of using `oauth2.Config.Exchange`.
- OAuth errors come back in two shapes: `{"error","error_description"}` for grant failures and `{"type","message"}` for rejected applications.
- Every catalog mutation requires an `idempotency_key`. Mise derives it deterministically from the operations in the batch (action, name, provider ID, location set, properties), so a retry after a network timeout replays the same key and Square returns the original result instead of creating duplicates.
- New objects in a batch-upsert are sent with a temporary `#name` ID; Square returns the real ID in `id_mappings`. That is also how objects within one batch reference each other.
- Catalog objects have a `version` field for optimistic concurrency — store it in state, pass it on updates. It arrives as a JSON number; Mise stores it as a string, since other platforms use opaque string etags.
- **Square's catalog is account-wide, not location-scoped.** One tax object carries the list of locations it applies at (`present_at_all_locations` minus `absent_at_location_ids`, or an explicit `present_at_location_ids`). The adapter lists each catalog type once per provider instance and filters per location in memory — otherwise a 15-location fetch would download the whole catalog 15 times. Commands build a fresh provider, so the cache never outlives one run.
- `GET /v2/catalog/list` is cursor-paginated. Mise detects a repeating cursor on its second sighting rather than exhausting a page budget.
- Deleted catalog objects are tombstoned (`is_deleted: true`), not removed — filter them out.
