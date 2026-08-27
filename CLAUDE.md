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
make build        # Build binary to bin/mise
make run ARGS="plan"  # Run without building
make test         # Run all tests with race detection
make lint         # Run golangci-lint
make fmt          # Format all Go files
```

## Development Conventions

- Config files use YAML. State files use JSON.
- Resources reference each other with `ref(type.name)` syntax. Adapters emit `provider.Ref{ResourceType, ProviderID}` values inside a resource's properties rather than raw IDs or ref strings; the engine rewrites them into `ref(type.name)` once every resource has a config name. This keeps platform-specific knowledge (which fields are references) in the adapter and naming policy in the engine.
- Config names are derived by the engine: the platform's display name, slugified ("GA State Sales Tax" → `ga_state_sales_tax`). Collisions get a numeric suffix assigned in provider-ID order, so names stay stable across runs.
- `mise fetch` must be idempotent — fetching twice with nothing changed produces byte-identical files. That means deterministic ordering everywhere: sorted resources, sorted keys, sorted reference lists.
- The provider interface uses `context.Context` on all methods for cancellation and timeouts.
- Error handling follows Go conventions: return errors, don't panic. API errors are classified as retryable (429, 5xx) or non-retryable (4xx).
- Square sandbox (`https://connect.squareupsandbox.com/v2`) is used for all development and testing.
- Secrets live only in `.mise/credentials` (0600, gitignored). `mise.yaml` records the auth *method*, never a token. `SQUARE_ACCESS_TOKEN` / `MISE_SQUARE_ACCESS_TOKEN` override the stored file so CI never runs `mise init`.
- Interactive prompts must degrade to a clear error when stdin is not a terminal — never block, never silently take a default.
- Mise never deletes POS resources unless the operator explicitly passes `--destroy`. Safety-first default.
- State files and credentials (`.mise/`) are gitignored and must never be committed.

## Current Status

Phase 0 — Foundation, Milestones 1 and 2 complete.

- `mise init` — OAuth2 authorization-code flow (local callback listener, anti-CSRF state, token exchange and refresh) and personal access tokens, credential verification via `GET /v2/locations`, workspace scaffolding.
- `mise fetch` — reads the live catalog (items, categories, taxes, discounts, modifier lists) plus locations, deduplicates resources across locations, resolves cross-resource references, generates YAML config files, and writes `.mise/state.json`.

`plan`, `apply`, and `drift` are still stubs, each with a TODO comment mapping to the PRD milestone where it gets implemented.

## Known Design Decisions To Revisit

- **State stores resolved refs.** `.mise/state.json` records properties in the same shape as the config files, i.e. `"category": "ref(square_catalog_category.beverages)"`, not the raw provider ID. Drift (Milestone 5) compares stored state against live API reads, where references arrive as `provider.Ref` values — so drift must normalize before comparing, and should match live objects to state entries **by provider ID**, never by config name. Matching by name would be fragile: adding a resource whose slug collides with an existing one can shift the numeric suffixes.

## Milestone Sequence

1. **Skeleton** (done) — Project structure, cobra CLI, provider interface, `mise init` with Square OAuth2 and access token auth
2. **Fetch** (done) — `mise fetch` pulls live config from Square into YAML files and writes the initial state file
3. **Plan** — `mise plan` computes diffs between declared YAML and live state
4. **Apply** — `mise apply` pushes changes to Square via batch API
5. **Drift** — `mise drift` detects changes made outside Mise
6. **Polish** — Integration tests, error handling, documentation

## Square API Notes

- Base URL: `https://connect.squareup.com/v2` (prod), `https://connect.squareupsandbox.com/v2` (sandbox)
- Auth: Bearer token in Authorization header
- Rate limit: ~40 req/s. The client has a built-in token-bucket limiter set to 30/s.
- Catalog objects use `present_at_location_ids` for location scoping
- Batch upsert (`POST /v2/catalog/batch-upsert`) handles up to 10,000 objects — prefer this over individual calls during apply
- OAuth endpoints live at the host root (`/oauth2/authorize`, `/oauth2/token`), not under `/v2`. The token endpoint takes a JSON body and returns an RFC3339 `expires_at`, not `expires_in` seconds — which is why Mise does its own token exchange instead of using `oauth2.Config.Exchange`.
- OAuth errors come back in two shapes: `{"error","error_description"}` for grant failures and `{"type","message"}` for rejected applications.
- Every catalog mutation requires an `idempotency_key`
- Catalog objects have a `version` field for optimistic concurrency — store it in state, pass it on updates. It arrives as a JSON number; Mise stores it as a string, since other platforms use opaque string etags.
- **Square's catalog is account-wide, not location-scoped.** One tax object carries the list of locations it applies at (`present_at_all_locations` minus `absent_at_location_ids`, or an explicit `present_at_location_ids`). The adapter lists each catalog type once per provider instance and filters per location in memory — otherwise a 15-location fetch would download the whole catalog 15 times. Commands build a fresh provider, so the cache never outlives one run.
- `GET /v2/catalog/list` is cursor-paginated. Mise detects a repeating cursor on its second sighting rather than exhausting a page budget.
- Deleted catalog objects are tombstoned (`is_deleted: true`), not removed — filter them out.
