# Releasing profinetctl

Releases are cut by the **Release** workflow (`.github/workflows/release.yml`, the shared `otfabric/.github` `go-binary-release.yml@v2`): run it from the Actions tab with the version bump (`patch`, `minor`, `major`). It tags the version, builds the binaries for Linux (x86-64, ARM64, ARMv7), macOS (x86-64, ARM64) and Windows (x86-64, ARM64) with the version, tag, commit and build date in `profinetctl version`, and publishes them with SHA-256 checksums.

## go-profinet

profinetctl requires a released go-profinet version (go.mod). When a release needs a newer go-profinet, release go-profinet first, then `GOWORK=off go get github.com/otfabric/go-profinet@vX.Y.Z && GOWORK=off go mod tidy`.

## v0.1.1 (2026-10-26)

Built on go-profinet v0.1.2: AR state changes of a device in order, RPC
responses of many fragments no longer time out on a loaded host, BPF ports
return `ErrClosed` after Close. No change to commands, output or exit
codes.

## v0.1.0 (2026-10-26)

First version, built on go-profinet v0.1.1.

- DCP discovery and commissioning: `discover`, `identify`, `name get/set`, `ip get/set`, `signal`, `reset`.
- Records and identification: `record read/write`, `im read/write`, `diagnostics`, `device info`, `status`.
- Topology: `lldp neighbors`, `topology` (PDRealData of every device).
- GSDML: `gsdml inspect`, `validate`, `modules`, `submodules`, `generate`.
- IO-Controller from a GSDML file or a go-profinet model: `connect`, `ar`, `iocr`, `io status/monitor/statistics`, `alarms`, `disconnect`.
- Text, table and JSON output with stable exit codes ([docs/exitcodes.md](docs/exitcodes.md), [docs/json-output.md](docs/json-output.md)); verified against rt-labs p-net ([docs/EXAMPLES.md](docs/EXAMPLES.md)).

## Checklist

- `make check` passes locally; CI is green.
- [docs/exitcodes.md](docs/exitcodes.md) and [docs/json-output.md](docs/json-output.md) describe any change of exit codes or JSON fields; a breaking change is called out in the release notes.
- `SECURITY.md` lists the supported versions.
