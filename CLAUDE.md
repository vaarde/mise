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
- Resources reference each other with `ref(type.name)` syntax — the engine resolves these at plan/apply time.
- The provider interface uses `context.Context` on all methods for cancellation and timeouts.
- Error handling follows Go conventions: return errors, don't panic. API errors are classified as retryable (429, 5xx) or non-retryable (4xx).
- Square sandbox (`https://connect.squareupsandbox.com/v2`) is used for all development and testing.
- Mise never deletes POS resources unless the operator explicitly passes `--destroy`. Safety-first default.
- State files and credentials (`.mise/`) are gitignored and must never be committed.

## Current Status

Phase 0 — Foundation. The project has the full scaffold: CLI commands (stubbed), provider interface, Square adapter (stubbed), config loader, state manager, and engine types. Each stubbed function has a TODO comment mapping to the PRD milestone where it gets implemented.

## Milestone Sequence

1. **Skeleton** (done) — Project structure, cobra CLI, provider interface
2. **Fetch** — `mise fetch` pulls live config from Square into YAML files
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
- Every catalog mutation requires an `idempotency_key`
- Catalog objects have a `version` field for optimistic concurrency — store it in state, pass it on updates
