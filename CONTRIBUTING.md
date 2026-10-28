# Contributing to profinetctl

Thanks for your interest. profinetctl is the command-line front end of [go-profinet](https://github.com/otfabric/go-profinet): protocol behaviour belongs in go-profinet, presentation, flags and exit codes here.

## Getting started

```bash
git clone https://github.com/otfabric/profinetctl.git
cd profinetctl
make test
```

The Makefile sets `GOWORK=off`. To work on go-profinet and profinetctl together, add `replace github.com/otfabric/go-profinet => ../go-profinet` to go.mod locally; do not commit it.

## Before you open a pull request

```bash
make check
```

runs formatting (`fmt`, `fmt-check` with the gofmt of the Go version in go.mod), `staticcheck`, `golangci-lint`, `go vet`, the 32-bit typecheck, `govulncheck`, the tests with the race detector and the coverage gate (90 %).

## How the code is organized

| Path | Contents |
|------|----------|
| `cmd/root.go` | the command tree, global flags, `Run` (what the tests drive) |
| `cmd/backend.go` | the `Backend`: the OS (raw Ethernet via go-profinet's `l2/osport`, the UDP stack) or, in tests, a virtual network |
| `cmd/common.go` | target selection, DCP and RPC sessions, error classification |
| `cmd/discover.go`, `configure.go` | DCP commands |
| `cmd/records.go`, `device.go` | records, I&M, diagnosis, device info, status |
| `cmd/lldp.go` | LLDP neighbors and topology |
| `cmd/gsdml.go` | GSDML commands |
| `cmd/ar.go` | the commands that establish an AR as IO-Controller |
| `cmd/roadmap.go` | `hello listen`, `gsdml check`, `topology check`, the `--peer-check` setting of `connect` |
| `cmd/v03.go` | `gsdml gsdx`, `mrp status`, the `--mrp-role` setting of `connect` |
| `cmd/completion.go`, `internal/cli` | shell completion |
| `internal/errs` | typed errors: kind (exit code) and code |
| `internal/format`, `internal/runner` | text, table and JSON output, fatal errors |

## Conventions

- Every result type implements `MarshalTextOutput` and `TableHeaders`/`TableRows`; JSON field names are snake_case ([docs/json-output.md](docs/json-output.md)).
- Errors returned by commands are typed (`internal/errs`): pick the kind by who is at fault (the user: usage or invalid input; the host: transport; the device: timeout, refused, protocol). go-profinet errors go through `classify`.
- Commands that change a device validate every flag before sending anything.
- New commands get tests in `cmd/*_test.go` against the virtual network (`harness_test.go`): real go-profinet IO-Devices, no privileges, no hardware.
- Files carry `// SPDX-License-Identifier: MIT`.

## Commits and pull requests

Small, focused commits with messages that say why. Pull requests run CI (`.github/workflows/ci.yml`); keep it green.
