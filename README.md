# Mise

**Everything in its place, at every location.**

Mise is a configuration-as-code tool for restaurant POS platforms. It lets multi-location restaurant operators define, version-control, and deploy POS configuration (menus, tax rates, service charges, discounts) as declarative YAML — with plan/apply workflows, drift detection, and rollback via git.

## Status

🚧 **Phase 0 — Foundation.** Building the core engine and Square POS adapter.

All five commands — `init`, `fetch`, `plan`, `apply`, `drift` — work against Square (sandbox and production), with an integration suite that exercises them against a real sandbox account. Release packaging (goreleaser) is the remaining Phase 0 work.

## How It Works

```bash
# Initialize a workspace and connect to Square
mise init

# Pull your current POS configuration into YAML files
mise fetch

# Edit a tax rate in the YAML file, then preview the change
mise plan

# Apply the change across all affected locations
mise apply

# Check if anyone changed POS settings outside of Mise
mise drift
```

## Quick Start

### Prerequisites

- Go 1.22+
- A Square developer account ([developer.squareup.com](https://developer.squareup.com))

### Build from source

```bash
git clone https://github.com/vaarde/mise.git
cd mise
make install     # puts `mise` on your PATH via GOPATH/bin
mise version
```

Or build into the repo without installing:

```bash
make build
./bin/mise --help
```

### Connect to Square

`mise init` creates the workspace and stores your credentials. It supports two
authentication methods.

**Access token** — simplest for a single operator or a sandbox account. Copy the
token from your application's *Credentials* page in the Square Developer
Dashboard:

```bash
mise init --environment sandbox --auth-method access_token
```

The token is read from a hidden prompt, or from `SQUARE_ACCESS_TOKEN` /
`MISE_SQUARE_ACCESS_TOKEN` if either is set.

**OAuth2** — best for multi-location accounts. Mise opens Square's consent
screen, then catches the redirect on a local listener:

```bash
mise init --environment sandbox --auth-method oauth2 \
  --client-id "$SQUARE_APPLICATION_ID" \
  --client-secret "$SQUARE_APPLICATION_SECRET"
```

Register the redirect URL `http://localhost:8666/mise/callback` under **OAuth**
in your Square application first. Use `--callback-port` if that port is taken
(and register the matching URL). Square requires HTTPS redirect URLs for
production applications, so production accounts generally use an access token.

Either way, Mise verifies the credentials by listing your locations before it
writes anything — a rejected token leaves no workspace behind. On success you
get:

```
mise.yaml            provider settings and location groups (commit this)
.mise/credentials    the token, permissions 0600 (never commit this)
.gitignore           updated to exclude .mise/ secrets and state
```

Non-interactive setups (CI, scripts) can supply every value by flag or
environment variable; Mise fails with an actionable error rather than blocking
on a prompt when there is no terminal.

### Import your current configuration

```bash
mise fetch
```

Fetch reads every location on the account and every catalog resource — items,
categories, taxes, discounts, modifier lists — and writes them out as YAML:

```
locations.yaml       your locations and their IDs (reference data)
taxes.yaml           tax rates
discounts.yaml       discount definitions
menu/categories.yaml
menu/items.yaml
menu/modifiers.yaml
.mise/state.json     name → provider ID mapping, for drift detection
```

A resource that exists at every location is written as `${group.all}`; one with
a partial rollout gets an explicit location list:

```yaml
resources:
  - type: square_catalog_tax
    name: ga_state_sales_tax
    locations: ${group.all}
    properties:
      name: GA State Sales Tax
      percentage: "4.5"
      enabled: true
      calculation_phase: TAX_SUBTOTAL_PHASE
      inclusion_type: ADDITIVE
```

Resources that point at each other keep that relationship by name rather than
by opaque ID, so the files stay readable and portable:

```yaml
  - type: square_catalog_item
    name: summer_lemonade
    locations: ${group.all}
    properties:
      name: Summer Lemonade
      category: ref(square_catalog_category.beverages)
      tax_ids:
        - ref(square_catalog_tax.ga_state_sales_tax)
```

Fetching twice with nothing changed produces byte-identical files, so a fetch
never shows up as a spurious diff in git. Because fetch regenerates these files
from the live POS, it prompts before overwriting them — pass `--force` to skip
the prompt, and `--parallelism N` to change how many locations are read at once
(default 10). It never touches `mise.yaml`.

Commit the result. That git history is your rollback mechanism.

### Preview a change

Edit a YAML file, then see exactly what would change before anything is touched:

```bash
mise plan
```

```
Mise will perform the following actions:

  ~ square_catalog_tax.ga_state_sales_tax (3 locations)
      percentage: "4.0" → "4.5"

  + square_catalog_tax.summer_promo_tax (2 locations)
      name       = "Summer Promo Tax"
      percentage = "2.5"

Plan: 1 to add, 1 to change, 0 to destroy.
```

Plan reads only — it writes nothing, not even state. Resources that exist on
the POS but not in your config files are left alone; Mise manages what you
declare and never proposes a destroy.

Changing which locations a resource applies at shows up too, so widening a tax
from three locations to forty is visible before you approve it.

Flags: `--target <name>` for one resource, `--location <name-or-id>` for one
location, and `--out <file>` to save the plan for a later `mise apply --plan`.

### Push the change

```bash
mise apply
```

Apply shows the same plan, asks for confirmation, then writes the changes:

```
Do you want to apply these changes? [y/N]: y

Applying changes...

  + square_catalog_tax.summer_promo_tax
  ~ square_catalog_tax.ga_state_sales_tax

Apply complete. 1 added, 1 changed, 0 destroyed.
```

Resources are written in dependency order — a tax exists before the menu item
that charges it — and independent resources go up in a single batch, so a menu
change across forty locations is one API call rather than hundreds.

If part of an apply fails, the resources that succeeded are still recorded in
state and the failures are named individually. Re-running only attempts what is
left; the idempotency keys make that safe.

Flags: `--auto-approve` for CI, `--plan <file>` to apply a saved plan,
`--target <name>` for one resource, `--parallelism <n>` to limit concurrency.

Mise never deletes POS resources. A resource removed from your config files is
left alone on the platform.

### Catch changes made behind your back

```bash
mise drift
```

Compares the live POS against the last configuration Mise recorded, and reports
anything someone changed elsewhere — a rate edited in the POS dashboard, a
discount deleted by hand, another tool writing to the same account:

```
Drift detected:

  - square_catalog_discount.happy_hour (2 locations)
      no longer exists on the POS
      ⚠ Deleted outside of Mise

  ~ square_catalog_tax.tn_sales_tax (Nashville)
      percentage: "9.75" (expected) → "9.25" (actual)
      ⚠ Changed outside of Mise

2 resources drifted across 2 locations.
Baseline: last applied 2026-09-16 09:00:00 UTC
```

Drift reads only. It never touches the POS and never rewrites state — a drift
report is evidence of a discrepancy, not permission to accept it. To accept the
live values as the new baseline, run `mise fetch`; to push your config back over
them, run `mise apply`.

It exits **2** when drift is found, distinct from **1** for a failure, so a
scheduled check can alert without parsing the output:

```bash
mise drift --json > report.json || [ $? -eq 2 ] && alert-someone < report.json
```

Flags: `--location <name-or-id>`, `--type <resource_type>`, `--json`.

### Roll back

Configuration is YAML in git, so rollback is a git operation:

```bash
git revert HEAD
mise apply
```

## Exit Codes

| Code | Meaning |
|-----:|---------|
| `0` | Success. For `drift`, no drift was found. |
| `1` | The command failed — bad credentials, an API error, invalid config. |
| `2` | `mise drift` only: drift was detected. Not a failure. |
| `130` | Interrupted with Ctrl-C. |

`drift` separates 2 from 1 so a scheduled check can tell "someone changed the
POS" from "the check itself broke". Everything else follows the usual
convention.

## Interrupting a Run

Ctrl-C during `apply` is handled rather than fatal. The first one stops after
the current step: in-flight requests are cancelled, the workspace lock is
released, and state is written for every resource that already landed — so a
re-run picks up where it left off instead of creating duplicates. A second
Ctrl-C quits immediately.

If an apply is interrupted *while a write is in flight*, Mise says so rather
than guessing:

```
apply interrupted while writing 3 resources: whether the change reached the
POS is unknown — run 'mise drift' before retrying
```

The request may have been applied with the response lost in transit. Recording
those as failures would send you to re-create resources that already exist, so
Mise reports the uncertainty and points at the command that resolves it.

## Troubleshooting

**`401 UNAUTHORIZED`** — the token is expired, revoked, or for the wrong
environment. Sandbox tokens do not work against production and vice versa;
check `provider.environment` in `mise.yaml`, then `mise init --force`.

**`403 FORBIDDEN`** — the token is valid but under-scoped. Reading needs
`ITEMS_READ` and `MERCHANT_PROFILE_READ`; `apply` also needs `ITEMS_WRITE`.

**`429 TOO_MANY_REQUESTS`** — Mise retries these automatically, honouring
Square's `Retry-After`. If it persists, lower `--parallelism`.

**`workspace is locked`** — another `apply` is running, or one was killed
outright. Locks older than ten minutes are broken automatically; otherwise
delete `.mise/lock` once you have confirmed nothing else is running.

**`no state to compare against`** — `drift` and `plan` need a baseline. Run
`mise fetch` first.

**Plan shows changes right after a fetch** — that should never happen and is a
bug worth reporting. It means fetch and plan disagree about the shape of a
resource.

## Testing

```bash
make test               # unit tests — no network, no credentials
make test-race          # adds race detection (needs cgo and a C compiler)
make cover              # per-package coverage
```

Integration tests run the real commands against a real Square **sandbox**
account. They are behind a build tag and skip without a token, so the default
suite never touches the network:

```bash
SQUARE_ACCESS_TOKEN=EAAAl... make test-integration
```

They create, modify, and delete catalog objects, and clean up after
themselves — point them only at a sandbox account. They cover what mocks
cannot: that a second `fetch` is byte-identical, that a re-`apply` is a no-op
rather than a duplicate create, that `drift` catches a change made in the
Square dashboard, and that the rate limiter survives a sustained burst.

## Project Structure

```
mise/
├── cmd/                          # CLI commands (cobra)
├── internal/
│   ├── config/                   # YAML config file parsing
│   ├── state/                    # State file management
│   ├── engine/                   # Plan/apply/drift logic
│   ├── provider/                 # Provider interface + registry
│   ├── credentials/              # .mise/credentials store
│   └── providers/
│       └── square/               # Square POS adapter (client, OAuth2, locations)
├── pkg/
│   └── output/                   # CLI output formatting
├── main.go
├── mise.yaml                     # Example config (created by mise init)
└── Makefile
```

## License

Copyright © 2026 Dan Gyinaye Poku <dan.gyinaye@gmail.com>

Licensed under the [Apache License, Version 2.0](LICENSE). See [NOTICE](NOTICE) for attribution information.
