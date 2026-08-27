# Mise — Product Requirements Document

**Everything in its place, at every location.**

*Phase 0: Foundation — v0.1.0*
*Author: Dan / Vaarde Consulting*
*Date: August 2026*

---

## 1. Overview

Mise is a CLI tool that lets multi-location restaurant operators define, version-control, and deploy POS configuration as declarative YAML — with plan/apply workflows, drift detection, and rollback via git. Phase 0 delivers the core engine and a Square POS adapter to prove the model end-to-end.

---

## 2. Problem Statement

Multi-location restaurant operators manage POS configuration (menus, tax rates, service charges, discounts, modifiers) through GUI dashboards — one location at a time. A tax rate change across 40 locations means 40 manual updates. There is no version control, no preview of changes before they go live, no drift detection when someone modifies settings locally, and no rollback mechanism beyond manually re-entering old values.

This results in configuration inconsistencies across locations, tax compliance exposure (a 1% misconfiguration on $500K annual sales can produce $15,000+ in audit penalties over three years), and operational time measured in "hours, days, weeks" for routine updates.

---

## 3. Target Users (Phase 0)

**Primary:** Multi-location restaurant operators using Square POS (5–50 locations). Likely persona is the owner-operator, Director of IT, or franchise operations manager who is comfortable running a CLI tool or can delegate to someone on their team who is.

**Secondary:** Restaurant technology consultants and integrators who manage POS setups for multiple restaurant clients.

---

## 4. Full-Stack Development Toolbox

### 4.1 Language & Runtime

| Layer | Choice | Rationale |
|---|---|---|
| Core engine + CLI | **Go 1.22+** | Single-binary distribution, goroutine concurrency for parallel API calls, fast compile times, ecosystem alignment with Terraform/OpenTofu |
| Config file format | **YAML** | Familiar to operators and developers alike, well-supported in Go, no custom language to learn |
| State file format | **JSON** | Native Go marshaling, human-readable for debugging, git-diffable |

### 4.2 Go Libraries

| Concern | Library | Purpose |
|---|---|---|
| CLI framework | `github.com/spf13/cobra` | Subcommands, flags, help generation. Industry standard (used by kubectl, Hugo, GitHub CLI) |
| Configuration | `github.com/spf13/viper` | Reading mise.yaml, environment variable expansion, config file discovery |
| YAML parsing | `gopkg.in/yaml.v3` | Reading/writing config files |
| HTTP client | `net/http` (stdlib) | Square API calls. No external dependency needed — Go's stdlib HTTP client is production-grade |
| OAuth2 | `golang.org/x/oauth2` | Square OAuth2 authorization flow |
| Logging | `log/slog` (stdlib, Go 1.21+) | Structured logging with levels |
| Testing | `testing` (stdlib) + `github.com/stretchr/testify` | Unit and integration tests with assertions and mocking |
| Colored CLI output | `github.com/fatih/color` | Colored diffs in plan/drift output (green for add, red for destroy, yellow for change) |
| Table output | `github.com/olekukonez/tablewriter` | Formatted tables for drift reports and cross-location comparisons |
| Diff engine | `github.com/r3labs/diff/v3` | Struct-level diffing for computing plan/drift changes between declared and live state |

### 4.3 Development & Build Tools

| Tool | Purpose |
|---|---|
| `go build` / `go install` | Compilation. Produces a single `mise` binary |
| `goreleaser` | Cross-platform release builds (Linux, macOS, Windows) + homebrew tap generation |
| `golangci-lint` | Linting — enforces consistent code style |
| `go test -race` | Race condition detection during tests |
| GitHub Actions | CI/CD — run tests, lint, and build on every push |
| `git` | Version control for both Mise's source code and the operator's config repos |

### 4.4 Square Integration

| Component | Detail |
|---|---|
| API version | Square API v2 (REST/JSON) |
| Authentication | OAuth2 (recommended for multi-location) or personal access token (for single-operator use) |
| Base URL | `https://connect.squareup.com/v2` (production), `https://connect.squareupsandbox.com/v2` (sandbox) |
| Rate limits | ~40 requests/second across all endpoints. Mise implements a token-bucket rate limiter |
| Sandbox | Square provides a free sandbox environment with test data — use this for all development and testing |

### 4.5 Future Layers (Out of Scope for Phase 0)

| Layer | Planned choice | Phase |
|---|---|---|
| Web UI frontend | React + TypeScript (Vite) | Phase 1–2 |
| Web UI backend | Go HTTP server (same codebase as CLI engine, exposed as an API) | Phase 1–2 |
| Database | PostgreSQL (multi-tenant state storage, user accounts) | Phase 2–3 |
| Auth | OAuth2 / OIDC (for the web UI) | Phase 2–3 |
| Toast adapter | Toast Configuration API v2 (read-only initially, write via partner program) | Phase 1–2 |

---

## 5. System Architecture

### 5.1 High-Level Component Diagram

```
┌─────────────────────────────────────────────────────┐
│                    Mise CLI                          │
│  (cobra commands: init, fetch, plan, apply, drift)  │
└──────────────────────┬──────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────┐
│                  Core Engine                         │
│                                                     │
│  ┌─────────┐  ┌───────────┐  ┌───────────────────┐ │
│  │  Config  │  │   State   │  │   Diff / Plan     │ │
│  │  Loader  │  │  Manager  │  │   Engine          │ │
│  └────┬─────┘  └─────┬─────┘  └────────┬──────────┘ │
│       │              │                 │            │
│       ▼              ▼                 ▼            │
│  ┌─────────────────────────────────────────────┐    │
│  │           Provider Interface                │    │
│  │  (Read, Create, Update, Delete per resource)│    │
│  └──────────────────┬──────────────────────────┘    │
└─────────────────────┼───────────────────────────────┘
                      │
          ┌───────────┴────────────┐
          ▼                        ▼
┌──────────────────┐    ┌──────────────────┐
│  Square Adapter  │    │  Toast Adapter   │
│  (Phase 0)       │    │  (Phase 1)       │
│                  │    │                  │
│  Catalog API     │    │  Config API      │
│  Locations API   │    │  Labor API       │
│  OAuth2          │    │  Ordering API    │
└──────────────────┘    └──────────────────┘
```

### 5.2 Directory Structure

```
mise/
├── cmd/                        # CLI command definitions (cobra)
│   ├── root.go                 # Root command, global flags
│   ├── init.go                 # mise init
│   ├── fetch.go                # mise fetch
│   ├── plan.go                 # mise plan
│   ├── apply.go                # mise apply
│   └── drift.go                # mise drift
├── internal/
│   ├── config/                 # Config file parsing (YAML → Go structs)
│   │   ├── loader.go           # Reads mise.yaml and resource files
│   │   ├── types.go            # Config struct definitions
│   │   └── validation.go       # Schema validation
│   ├── state/                  # State file management
│   │   ├── state.go            # Read/write mise.state.json
│   │   ├── types.go            # State struct definitions
│   │   └── lock.go             # File-based state locking
│   ├── engine/                 # Core plan/apply/drift logic
│   │   ├── plan.go             # Computes diff: declared vs live
│   │   ├── apply.go            # Executes changes via provider
│   │   ├── drift.go            # Computes diff: state vs live
│   │   ├── graph.go            # Dependency resolution (DAG)
│   │   └── types.go            # ResourceChange, Plan structs
│   ├── provider/               # Provider interface + registry
│   │   ├── interface.go        # Provider interface definition
│   │   └── registry.go         # Maps platform names to adapters
│   └── providers/
│       └── square/             # Square adapter
│           ├── provider.go     # Implements Provider interface
│           ├── client.go       # Square API HTTP client
│           ├── auth.go         # OAuth2 flow
│           ├── resources.go    # Resource type registry
│           ├── catalog_item.go # square_catalog_item CRUD
│           ├── catalog_tax.go  # square_catalog_tax CRUD
│           ├── catalog_category.go
│           ├── catalog_discount.go
│           ├── catalog_modifier.go
│           ├── location.go     # square_location read/update
│           └── rate_limiter.go # Token-bucket rate limiter
├── pkg/
│   └── output/                 # CLI output formatting
│       ├── plan_printer.go     # Colored plan output
│       ├── drift_printer.go    # Drift report formatting
│       └── table.go            # Cross-location comparison tables
├── mise.yaml                   # Example root config
├── go.mod
├── go.sum
├── main.go                     # Entry point
├── Makefile                    # Build, test, lint shortcuts
└── README.md
```

### 5.3 Provider Interface

This is the contract every POS adapter must implement. The core engine only talks to this interface — it never calls Square or Toast APIs directly.

```go
// internal/provider/interface.go

type Provider interface {
    // Name returns the provider identifier ("square", "toast")
    Name() string

    // Configure initializes the provider with credentials and settings
    Configure(config map[string]interface{}) error

    // ListLocations returns all locations/restaurants in the account
    ListLocations() ([]Location, error)

    // ResourceTypes returns the resource types this provider supports
    ResourceTypes() []string

    // Read fetches the current live state of a resource by type and ID
    Read(resourceType string, id string, locationID string) (*Resource, error)

    // ReadAll fetches all resources of a given type at a location
    ReadAll(resourceType string, locationID string) ([]*Resource, error)

    // Create creates a new resource and returns its provider-assigned ID
    Create(resourceType string, desired *Resource, locationID string) (string, error)

    // Update modifies an existing resource
    Update(resourceType string, id string, desired *Resource, locationID string) error

    // Delete removes a resource
    Delete(resourceType string, id string, locationID string) error
}

type Location struct {
    ID       string            // Provider-assigned ID (Square location ID, Toast GUID)
    Name     string            // Human-readable name ("Atlanta - Peachtree St")
    Address  string
    Metadata map[string]string // Provider-specific fields (state, timezone, etc.)
}

type Resource struct {
    Type       string                 // e.g. "square_catalog_tax"
    Name       string                 // User-defined name from config file
    ProviderID string                 // Provider-assigned ID (from API)
    Properties map[string]interface{} // Resource-specific properties
    LocationID string                 // Which location this belongs to
}
```

---

## 6. Core Features (Phase 0 Scope)

### 6.1 `mise init`

**Purpose:** Initialize a new Mise workspace.

**Behavior:**
1. Prompts for platform selection (square).
2. Prompts for authentication method (OAuth2 or access token).
3. For OAuth2: opens browser for Square authorization, receives callback, stores token securely.
4. For access token: prompts for token, validates it against the API.
5. Creates `mise.yaml` with provider configuration.
6. Creates `.mise/` directory for state and internal files.
7. Creates `.gitignore` entry for `.mise/state.json` (state contains provider IDs, should not be shared by default) and `.mise/credentials` (must never be committed).

**Output:** A workspace directory ready for `mise fetch`.

### 6.2 `mise fetch`

**Purpose:** Pull the current live configuration from the POS and generate YAML config files.

**Behavior:**
1. Reads `mise.yaml` for provider configuration.
2. Calls `provider.ListLocations()` to discover all locations.
3. For each location, calls `provider.ReadAll()` for each supported resource type.
4. Groups resources by type and generates YAML files:
   - `locations.yaml` — all locations with their metadata
   - `taxes/` — one file per tax jurisdiction or one combined file
   - `menu/` — menu items, categories, modifier lists
   - `discounts.yaml` — discount definitions
5. Writes initial state file (`.mise/state.json`) mapping resource names to provider IDs.
6. Prints summary: "Fetched 847 resources across 15 locations."

**Concurrency:** Locations are fetched in parallel using goroutines (one goroutine per location, bounded by a semaphore matching the API rate limit).

**Idempotency:** Running `fetch` twice produces identical output if nothing changed. Running `fetch` after manual edits to config files will overwrite those edits — the CLI warns: "This will overwrite existing config files. Proceed? [y/N]"

### 6.3 `mise plan`

**Purpose:** Compare declared config files against live POS state and show what would change.

**Behavior:**
1. Loads declared state from YAML config files.
2. Loads live state by calling `provider.ReadAll()` for each resource type at each relevant location.
3. Computes a diff using the diff engine:
   - Resources in config but not live → **create**
   - Resources in both but with different properties → **update** (shows property-level diff)
   - Resources in live but not in config → **no action** (Mise does not destroy resources unless explicitly told to, for safety)
4. Prints a colored plan output.

**Example output:**
```
Mise will perform the following actions:

  ~ square_catalog_tax.ga_state_sales_tax (3 locations)
      percentage: "4.0" → "4.5"

  + square_catalog_item.summer_lemonade (15 locations)
      name:     "Summer Lemonade"
      price:    450
      category: ref(square_catalog_category.beverages)

Plan: 1 to add, 1 to change, 0 to destroy.
```

**Flags:**
- `--target <resource_name>` — plan only for a specific resource
- `--location <location_name>` — plan only for a specific location
- `--out <file>` — save plan to a file for later apply

### 6.4 `mise apply`

**Purpose:** Execute the changes described by a plan.

**Behavior:**
1. Runs a plan internally (or reads a saved plan from `--plan <file>`).
2. Prints the plan summary and prompts: "Do you want to apply these changes? [y/N]"
3. On confirmation, executes changes via the provider:
   - Resolves dependency order using a DAG (directed acyclic graph). Tax rates are created before menu items that reference them. Categories before items in those categories.
   - Creates/updates resources in dependency order, per location.
   - Parallelizes across locations (bounded by rate limiter).
4. Updates the state file with new provider IDs and property values.
5. Prints result: "Apply complete. 1 added, 1 changed, 0 destroyed."
6. On partial failure: reports which locations succeeded and which failed, updates state only for successful changes, and exits with a non-zero code.

**Flags:**
- `--auto-approve` — skip confirmation prompt (for CI/CD use)
- `--plan <file>` — apply a previously saved plan
- `--target <resource_name>` — apply only a specific resource
- `--parallelism <n>` — limit concurrent API calls (default: 10)

### 6.5 `mise drift`

**Purpose:** Detect configuration changes made outside of Mise (via POS dashboard, another tool, etc.).

**Behavior:**
1. Loads the last-known state from `.mise/state.json`.
2. Fetches the current live state from the POS API.
3. Compares live state against the stored state.
4. Reports differences per location.

**Example output:**
```
Drift detected:

  Location: Nashville (loc_abc123)
  ~ square_catalog_tax.tn_sales_tax
      percentage: "9.75" (expected) → "9.25" (actual)
      ⚠ Changed outside of Mise

  Location: Denver (loc_def456)
  ~ square_catalog_discount.happy_hour
      percentage: "15" (expected) → "20" (actual)
      ⚠ Changed outside of Mise

2 resources drifted across 2 locations.
```

**Flags:**
- `--location <name>` — check drift for a single location
- `--type <resource_type>` — check drift for a specific resource type only
- `--json` — output drift report as JSON (for programmatic consumption)

---

## 7. Data Models

### 7.1 Root Config File (`mise.yaml`)

```yaml
version: "1"

provider:
  platform: square
  environment: production       # "production" or "sandbox"
  credentials:
    method: oauth2              # "oauth2" or "access_token"
    # Actual tokens stored in .mise/credentials, never in this file

location_groups:
  all:
    filter: "*"
  georgia:
    filter:
      state: "GA"
  west_coast:
    filter:
      state: ["CA", "OR", "WA"]
  flagship:
    filter:
      ids: ["loc_abc123"]
```

### 7.2 Resource Config Files

```yaml
# taxes/georgia.yaml
resources:
  - type: square_catalog_tax
    name: ga_state_sales_tax
    locations: ${group.georgia}
    properties:
      name: "GA State Sales Tax"
      calculation_phase: TAX_SUBTOTAL_PHASE
      inclusion_type: ADDITIVE
      percentage: "4.5"
      enabled: true
      applies_to_custom_amounts: true

  - type: square_catalog_tax
    name: ga_alcohol_tax
    locations: ${group.georgia}
    properties:
      name: "GA Alcohol Tax"
      calculation_phase: TAX_SUBTOTAL_PHASE
      inclusion_type: ADDITIVE
      percentage: "6.0"
      enabled: true
```

```yaml
# menu/beverages.yaml
resources:
  - type: square_catalog_category
    name: beverages
    locations: ${group.all}
    properties:
      name: "Beverages"

  - type: square_catalog_item
    name: summer_lemonade
    locations: ${group.all}
    properties:
      name: "Summer Lemonade"
      description: "Fresh-squeezed lemonade with mint"
      category: ref(square_catalog_category.beverages)
      tax_ids:
        - ref(square_catalog_tax.ga_state_sales_tax)
      variations:
        - name: "Regular"
          price_money:
            amount: 450
            currency: USD
        - name: "Large"
          price_money:
            amount: 595
            currency: USD
```

### 7.3 State File (`.mise/state.json`)

```json
{
  "version": 1,
  "provider": "square",
  "last_fetch": "2026-09-15T14:23:00Z",
  "last_apply": "2026-09-15T14:30:00Z",
  "resources": {
    "square_catalog_tax.ga_state_sales_tax": {
      "provider_id": "ZMBC7XTQAHBBXE3AOCPZX5LN",
      "type": "square_catalog_tax",
      "name": "ga_state_sales_tax",
      "locations": ["loc_abc123", "loc_def456"],
      "properties": {
        "name": "GA State Sales Tax",
        "percentage": "4.5",
        "calculation_phase": "TAX_SUBTOTAL_PHASE",
        "inclusion_type": "ADDITIVE",
        "enabled": true
      },
      "last_synced": "2026-09-15T14:30:00Z"
    }
  },
  "locations": {
    "loc_abc123": {
      "name": "Atlanta - Peachtree St",
      "state": "GA",
      "timezone": "America/New_York"
    }
  }
}
```

### 7.4 Resource Reference System

Resources can reference other resources using `ref()`:

```yaml
tax_ids:
  - ref(square_catalog_tax.ga_state_sales_tax)
```

The engine resolves `ref()` at plan/apply time by looking up the provider ID from the state file. This also builds the dependency graph — if resource A references resource B, then B must be created before A during apply.

---

## 8. Square API Integration Details

### 8.1 Resource Type Mapping

| Mise Resource Type | Square API Endpoint | Operations |
|---|---|---|
| `square_location` | `GET /v2/locations`, `PUT /v2/locations/{id}` | Read, Update (cannot create/delete locations via API) |
| `square_catalog_item` | `POST /v2/catalog/object` (type: ITEM) | Create, Read, Update, Delete |
| `square_catalog_category` | `POST /v2/catalog/object` (type: CATEGORY) | Create, Read, Update, Delete |
| `square_catalog_tax` | `POST /v2/catalog/object` (type: TAX) | Create, Read, Update, Delete |
| `square_catalog_discount` | `POST /v2/catalog/object` (type: DISCOUNT) | Create, Read, Update, Delete |
| `square_catalog_modifier_list` | `POST /v2/catalog/object` (type: MODIFIER_LIST) | Create, Read, Update, Delete |

### 8.2 Batch Operations

Square's Catalog API supports batch operations (`POST /v2/catalog/batch-upsert`, `POST /v2/catalog/batch-delete`) which Mise should prefer over individual calls when applying changes to multiple items. A single batch-upsert can handle up to 10,000 objects. This significantly reduces API calls during large applies.

### 8.3 Idempotency

Square's Catalog API uses an `idempotency_key` on create/update operations. Mise generates deterministic idempotency keys from the resource name + location ID + a hash of the desired properties. This ensures that a retried apply (after partial failure) doesn't create duplicate resources.

### 8.4 Version Tokens

Square catalog objects include a `version` field that changes on every update. Mise stores this in the state file and passes it on updates to ensure it doesn't overwrite changes made between plan and apply. If the version doesn't match, the update fails with a conflict error, and Mise reports: "Resource was modified since last fetch. Run `mise drift` to see changes."

### 8.5 Location Scoping

Square catalog objects can be `present_at_all_locations` or present at a specific set of `present_at_location_ids`. Mise maps location groups to these fields. When a resource declares `locations: ${group.georgia}`, the adapter resolves the group to specific location IDs and sets `present_at_location_ids` accordingly.

---

## 9. Error Handling

**API errors:** Mise categorizes API errors as retryable (429 rate limit, 500/503 server errors) and non-retryable (400 bad request, 401 unauthorized, 404 not found). Retryable errors are retried with exponential backoff (max 3 attempts). Non-retryable errors are reported immediately with the full API error message.

**Partial apply failure:** If an apply succeeds at 38 of 40 locations, Mise updates the state file for the 38 successful locations, reports the 2 failures with location names and error details, and exits with code 1. The operator can fix the issue and run `mise apply` again — it will only attempt the 2 remaining locations (since the other 38 already match the declared state).

**State corruption:** If the state file is deleted or corrupted, `mise fetch` re-imports the live state from scratch. Mise never assumes the state file is authoritative — it's a cache of known provider IDs and last-known values, not the source of truth. The YAML config files and the live POS are the two sources of truth; the state file bridges them.

**Concurrent access:** Phase 0 uses file-based locking (`.mise/lock`) to prevent two `mise apply` processes from running simultaneously. The lock file contains the PID and timestamp. Stale locks (older than 10 minutes) are automatically broken.

---

## 10. Out of Scope (Phase 0)

- Toast adapter (Phase 1)
- Web UI (Phase 1–2)
- Remote state backend (Phase 3)
- CI/CD integration (Phase 3)
- Policy-as-code (Phase 3)
- Scheduled drift detection (Phase 3)
- User accounts, authentication, multi-tenancy (Phase 2–3)
- Resource destruction (Mise does not delete resources from the POS unless a `--destroy` flag is explicitly passed — safety-first default)
- Import of individual resources (Phase 0 only supports full `fetch`; selective import comes later)

---

## 11. Success Criteria

**Technical validation (Month 2):**
- Mise can `fetch` a Square sandbox account with 5 locations, 50+ catalog items, and 5+ tax rates into clean YAML files.
- Mise can `plan` a tax rate change and produce a correct, human-readable diff.
- Mise can `apply` the change across all 5 locations via Square's batch API.
- Mise can `drift` and detect a change made through the Square Dashboard.
- Round-trip fidelity: `fetch` → make no changes → `plan` shows "No changes."

**User validation (Month 3):**
- 5 Square restaurant operators (real or recruited via restaurant tech communities) have used Mise for at least one real plan/apply cycle.
- At least 1 operator has discovered a configuration inconsistency via `mise drift` that they didn't know about.
- The fetch-to-first-apply workflow completes in under 10 minutes for a new user.

---

## 12. Development Milestones

**Milestone 1 — Skeleton (Week 1–2):**
Go project scaffolding, cobra CLI with all 5 subcommands (stubbed), Square API client with OAuth2, `mise init` working against Square sandbox.

**Milestone 2 — Fetch (Week 3–4):**
`mise fetch` pulls catalog items, categories, taxes, discounts, and modifiers from Square sandbox. Generates clean YAML files. Writes initial state file.

**Milestone 3 — Plan (Week 5–6):**
Config loader parses YAML files. Diff engine computes changes. `mise plan` produces colored diff output. Handles `ref()` resolution and dependency ordering.

**Milestone 4 — Apply (Week 7–8):**
`mise apply` executes creates and updates via Square's batch API. Handles partial failures. Updates state file. Idempotency keys work correctly on retry.

**Milestone 5 — Drift (Week 9–10):**
`mise drift` compares stored state against live API state. Produces drift report. Correctly identifies changes made outside Mise.

**Milestone 6 — Polish & Test (Week 11–12):**
End-to-end integration tests against Square sandbox. Error handling hardened. README and documentation written. First release via goreleaser.
