# Releasing profinetctl

Releases are cut by the **Release** workflow (`.github/workflows/release.yml`, the shared `otfabric/.github` `go-binary-release.yml@v2`): run it from the Actions tab with the version bump (`patch`, `minor`, `major`). It tags the version, builds the binaries for Linux (x86-64, ARM64, ARMv7), macOS (x86-64, ARM64) and Windows (x86-64, ARM64) with the version, tag, commit and build date in `profinetctl version`, and publishes them with SHA-256 checksums.

## go-profinet

profinetctl requires a released go-profinet version (go.mod). When a release needs a newer go-profinet, release go-profinet first, then `GOWORK=off go get github.com/otfabric/go-profinet@vX.Y.Z && GOWORK=off go mod tidy`.

## Notes

The notes of a version are the body of its GitHub release: the Release
workflow (the shared `go-binary-release.yml`) takes everything after the
heading `## vX.Y.Z` up to the next line that is exactly `---`. One section
per version, newest first, the heading exactly `## vX.Y.Z`, each section
ending with a `---` line and no `---` line inside a section
(`make release-notes-check`).

## Checklist

- `make check` passes locally (it includes `make release-notes-check`); CI is green.
- [docs/exitcodes.md](docs/exitcodes.md) and [docs/json-output.md](docs/json-output.md) describe any change of exit codes or JSON fields; a breaking change is called out in the release notes.
- `SECURITY.md` lists the supported versions.

---

## v0.3.1

**Date:** 2026-10-28
**Previous release:** v0.3.0

## Summary

Requires go-profinet v0.5.0 (MRP automatic manager, MRP verified against
the Linux kernel's bridge MRP).

## Changes

- go-profinet v0.5.0: a device's unchanged state is no longer saved again,
  and `mrp status` of a go-profinet device configured as an automatic
  manager (MRA) shows the role it acts in.
- README: MRA cannot be set with `--mrp-role` (no PROFINET MRP_Role value).

---

## v0.3.0

**Date:** 2026-10-28
**Previous release:** v0.2.0

## Summary

Requires go-profinet v0.4.0. `connect --reparam` re-parameterizes a
running AR; the limitations say what profinetctl does with MRP, shared
devices and system redundancy.

## Changes

- `connect --reparam SLOT:SUBSLOT:INDEX:HEX` (repeatable) and
  `--reparam-after` (default 1 s): PrmBegin, the records, PrmEnd and the
  device's ApplicationReady while cyclic data goes on; a `reparameterized`
  event (with `error` when it failed) in the stream.
- README limitations corrected: profinetctl gives a multiport device its
  MRP role and reads its state, and expects a subset of modules on a shared
  device (it said "no MRP, no shared devices"); S2 is not exposed.
- go-profinet v0.4.0: an allocation-free AF_PACKET backend on Linux, stable
  LLDP neighbor indexes in the device's SNMP agent, and fixes (a second
  controller could be refused "out of AR resources").

---

## v0.2.0

**Date:** 2026-10-27
**Previous release:** v0.1.2

Built on go-profinet v0.3.0
([release notes](https://github.com/otfabric/go-profinet/releases/tag/v0.3.0)):
commands for the features of its software-only roadmap. No change to the
output or exit codes of existing commands.

- `gsdml check FILE [--strict]`: the full GSDML check, every issue with its
  severity (error, warning, unvalidated) and rule; exit code 7 on errors,
  with `--strict` on any issue.
- `gsdml gsdx FILE`: inspect a GSDX container (parts, relationships, core
  properties, signature metadata; signatures are not verified, PI's GSDX
  rules are members only), each GSDML part checked; `--extract` writes one
  part.
- `topology check --link A/PORT=B/PORT | --expect FILE`: verify the cabling
  against the expected links; exit code 7 when it differs.
- `hello listen`: stream the DCP Hellos of devices that announce themselves.
- `mrp status`: a device's MRP role, domain, ring state and ring ports.
- `connect` and the AR commands: `--slot N` expects only some modules
  (shared device), `--peer-check PORT=STATION/PORT` writes a peer check,
  `--mrp-role client|manager|off` writes the MRP records of a multiport
  device.
- JSON of the new commands in [docs/json-output.md](docs/json-output.md).

---

## v0.1.2

**Date:** 2026-10-27
**Previous release:** v0.1.1

Built on go-profinet v0.2.0
([release notes](https://github.com/otfabric/go-profinet/releases/tag/v0.2.0)).

- `gsdml validate` also checks that the input and the output data of every
  submodule fit an RT frame (1439 octets plus the provider status): a file
  that breaks it now has issues (exit code 7). Its JSON issues gain
  `severity` (`"error"`) and `rule` (`"structure"`, `"reference"` or
  `"io-data-length"`); see [docs/json-output.md](docs/json-output.md). The
  `checked:` line says so.
- The IO-Controller commands (`connect`, `io`, `alarms`) run on go-profinet
  v0.2.0's controller, whose state waits now return only after the state's
  callback. No other change to commands, output or exit codes.
- RELEASE.md in the otfabric release-notes format, checked by
  `make release-notes-check` and CI.

---

## v0.1.1

**Date:** 2026-10-26
**Previous release:** v0.1.0

Built on go-profinet v0.1.2: AR state changes of a device in order, RPC
responses of many fragments no longer time out on a loaded host, BPF ports
return `ErrClosed` after Close. No change to commands, output or exit
codes.

---

## v0.1.0

**Date:** 2026-10-26

First version, built on go-profinet v0.1.1.

- DCP discovery and commissioning: `discover`, `identify`, `name get/set`, `ip get/set`, `signal`, `reset`.
- Records and identification: `record read/write`, `im read/write`, `diagnostics`, `device info`, `status`.
- Topology: `lldp neighbors`, `topology` (PDRealData of every device).
- GSDML: `gsdml inspect`, `validate`, `modules`, `submodules`, `generate`.
- IO-Controller from a GSDML file or a go-profinet model: `connect`, `ar`, `iocr`, `io status/monitor/statistics`, `alarms`, `disconnect`.
- Text, table and JSON output with stable exit codes ([docs/exitcodes.md](docs/exitcodes.md), [docs/json-output.md](docs/json-output.md)); verified against rt-labs p-net ([docs/EXAMPLES.md](docs/EXAMPLES.md)).

---
