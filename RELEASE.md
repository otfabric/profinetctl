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

## v0.7.1

**Date:** 2026-10-31
**Previous release:** v0.7.0

### Dependencies

- go-profinet v0.6.1 (see its release notes). No command changes.

---

## v0.7.0

**Date:** 2026-10-30
**Previous release:** v0.6.0

### Summary

**Breaking for scripts:** the exit codes follow the OT Fabric organisation table (ADR-0006 of otfabric/engineering). Nothing else changes in the commands.

### Exit codes

| Case | Old | New | JSON error |
|---|---|---|---|
| `permission_denied` | 4 | 9 | kind `permission`, was `transport` |
| `unsupported` | 6 | 8 | kind `unsupported`, was `refused` |
| `device_not_found` and other `not_found` errors | 8 | 11 | the kind is unchanged |

Every other exit code is unchanged. See [docs/exitcodes.md](docs/exitcodes.md).

### Environment

`PROFINETCTL_FORMAT` is now the canonical name of the format variable; `PROFINETCTL_OUTPUT_FORMAT` stays accepted, and `PROFINETCTL_FORMAT` wins when both are set.

---

## v0.6.0

**Date:** 2026-10-29
**Previous release:** v0.3.2

## Summary

Built on go-profinet v0.6.0 and released together with it and
profinet-interop v0.6.0: from this version the three share one version
number (there is no v0.4.x or v0.5.x of profinetctl). The examples cover
every command.

## Changes

- go-profinet v0.6.0: PNIO error statuses of CMRPC name their cause ("AR
  UUID unknown", "state conflict", …) in error messages, for example a
  `disconnect` of an AR the device does not know.
- `mrp status` of a device without media redundancy (its read answers
  "invalid index") fails as `unsupported` (exit 6) with a clear message
  instead of the raw PNIO status.
- `make examples-doc` also shows `hello listen` while a device starts,
  `alarms` with the diagnosis that follows, `mrp status` and
  `disconnect`; docs/EXAMPLES.md on main is regenerated from this
  release's binary.
- SECURITY.md: 0.6.x supported; docs/exitcodes.md and
  docs/json-output.md name the `mrp status` case.

---

## v0.3.2

**Date:** 2026-10-28
**Previous release:** v0.3.1

## Summary

`topology check` speaks text, and the documentation was checked command
by command against the code.

## Changes

- `topology check` with `--format text` writes one line per expected link
  (its verdict and what the ends report), the unexpected links and the
  unreachable stations; it printed the JSON report before.
- Stream events without detail (`reparameterized` that succeeded) end
  without a trailing space.
- docs/json-output.md: `device info` names `module_ident_number` and
  `submodule_ident_number` (as the code always did); `disconnect` and
  `gsdml generate` results; optional lists are omitted when empty.
- docs/exitcodes.md: exit 7 of `gsdml check`, `gsdml gsdx` and `topology
  check`; `hello listen` streams.
- README: the flags of `discover`, `record write`/`im write` in an IOCAR,
  `lldp neighbors`, `topology`, `mrp status`, `io`, and the AR flags of
  `connect`; GSDX inspection versus signature verification.
- SECURITY.md: 0.3.x supported; `connect --param/--reparam/--mrp-role/
  --peer-check/--assign-ip` among the commands that change a device.
- `make examples-doc` also shows `gsdml check`, `gsdml generate`,
  `topology check` and `connect --reparam`; docs/EXAMPLES.md on
  main is regenerated from this release's binary.

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
