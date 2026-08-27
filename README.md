# Mise

**Everything in its place, at every location.**

Mise is a configuration-as-code tool for restaurant POS platforms. It lets multi-location restaurant operators define, version-control, and deploy POS configuration (menus, tax rates, service charges, discounts) as declarative YAML — with plan/apply workflows, drift detection, and rollback via git.

## Status

🚧 **Phase 0 — Foundation.** Building the core engine and Square POS adapter.

`mise init` works against Square (sandbox and production). `fetch`, `plan`, `apply`, and `drift` are still stubs.

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
mise init --environment sandbox --auth-method oauth2           --client-id "$SQUARE_APPLICATION_ID"           --client-secret "$SQUARE_APPLICATION_SECRET"
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

## Project Structure

```
mise/
├── cmd/                          # CLI commands (cobra)
├── internal/
│   ├── config/                   # YAML config file parsing
│   ├── state/                    # State file management
│   ├── engine/                   # Plan/apply/drift logic
│   ├── provider/                 # Provider interface + registry
│   ├── credentials/               # .mise/credentials store
│   └── providers/
│       └── square/               # Square POS adapter (client, OAuth2, locations)
├── pkg/
│   └── output/                   # CLI output formatting
├── main.go
├── mise.yaml                     # Example config (created by mise init)
└── Makefile
```

## License

TBD
