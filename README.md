# Mise

**Everything in its place, at every location.**

Mise is a configuration-as-code tool for restaurant POS platforms. It lets multi-location restaurant operators define, version-control, and deploy POS configuration (menus, tax rates, service charges, discounts) as declarative YAML — with plan/apply workflows, drift detection, and rollback via git.

## Status

🚧 **Phase 0 — Foundation.** Building the core engine and Square POS adapter.

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

## Project Structure

```
mise/
├── cmd/                          # CLI commands (cobra)
├── internal/
│   ├── config/                   # YAML config file parsing
│   ├── state/                    # State file management
│   ├── engine/                   # Plan/apply/drift logic
│   ├── provider/                 # Provider interface + registry
│   └── providers/
│       └── square/               # Square POS adapter
├── pkg/
│   └── output/                   # CLI output formatting
├── main.go
├── mise.yaml                     # Example config (created by mise init)
└── Makefile
```

## License

TBD
