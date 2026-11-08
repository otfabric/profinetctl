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

## v0.10.0

**Date:** 2026-11-08
**Previous release:** v0.9.0

### Summary

profinetctl is built on go-profinet v0.8.0, which was validated against
traffic recorded from real equipment (a SIMATIC S7-1500 CPU 1515-2 PN and
older Siemens, GE and switch devices; go-profinet's testdata/captures).
`lldp neighbors` shows the management address's interface number and OID,
and the go-profinet fixes reach every command.

### Added

- `lldp neighbors` JSON: `management_if_index` and `management_oid` (dotted,
  for example `1.3.6.1.4.1.24686`, the PNO enterprise OID Siemens devices
  send), both omitted when absent ([docs/json-output.md](docs/json-output.md)).

### Changed (from go-profinet v0.8.0)

- `discover` and every DCP output: a NameOfStation or TypeOfStation padded
  with NULs (an S7-300 pads its name to 108 octets) is shown without the
  padding.
- `lldp neighbors`, `topology`: an LLDPDU with a second Chassis ID, Port ID
  or TTL TLV is ignored (IEEE 802.1AB); a non-IPv4 management address no
  longer hides the rest of the TLV list.
- `alarms`, `connect`, `io`: profinetctl as IO-Controller answers a
  device's ERR-RTA with ERR-RTA "AR alarm.ind(err)" (`CF 81 FD 0B`), as a
  CPU 1515-2 PN does.

### Dependencies

- go-profinet v0.7.0 → v0.8.0.

---

## v0.9.0

**Date:** 2026-11-07
**Previous release:** v0.8.0

### Summary

`version` prints one line, as every OT Fabric CLI does, or JSON with
`--json` (or `-o json`). Shell completion completes `--mrp-role`, the I&M
records and files by type, and no longer offers file names for addresses,
names and numbers.

### Added

- `version --json`, and `version` with `--format json`/`-o json` or
  `PROFINETCTL_FORMAT=json`: `{"name","version","tag","commit","built","libraries","go","platform"}`,
  every key present; `libraries` lists go-profinet with its version.
- Completion of `--mrp-role` (client, manager, off) and `im read --record`
  (0-4), next to `--format`, `--mode` and `--interface`. GSDML arguments
  and `--gsdml` complete `.xml` files, `gsdml gsdx` `.gsdx`, `--model` and
  `--expect` `.json`, `--pcap` `.pcap`; `--data-file` and `--output-file`
  any file. Every other value flag and argument completes nothing instead
  of file names.

### Changed

- `version` prints one line: `profinetctl 0.9.0 (tag v0.9.0, commit <sha>,
  built <date>, go-profinet 0.7.0, go1.26.10, linux/amd64)`, instead of one
  line per field. No leading `v`; the full commit (from the build, or from
  Go's build information with `-dirty` for a modified tree); the tag is left
  out when there is none; a development build (a pseudo-version too) is
  `dev`, built `unknown`. Scripts that read `profinetctl version: …` should
  use `version --json`.
- `make build` injects the full commit SHA, and a version and tag only when
  HEAD is exactly on a tag (otherwise the binary reports `dev`).

---

## v0.8.0

**Date:** 2026-11-05
**Previous release:** v0.7.2

Hardening from a gosec/opengrep review (8 findings at low/low: 4 fixed in
code; the file paths the operator names are 3 findings recorded as false
positives) and a hostile-input review. Built on go-profinet v0.7.0; Go 1.26 or later.

### Security

- Text and table output escape control characters and invalid UTF-8 in
  what devices send (station and type names, LLDP strings, I&M text,
  errors): a device can no longer put terminal escape sequences on the
  operator's screen, or add rows to a table. JSON output is unchanged.
- Files profinetctl creates (`--pcap`, `gsdml generate -O`,
  `gsdml gsdx --extract -O`) have mode 0600. A `--pcap` file is only
  emptied once the interface is open, so a failed open keeps an existing
  capture; a capture that could not be written completely is reported on
  stderr.
- Model, `--data-file` and `--expect` files above 16 MiB are refused.
- `topology` reads the port data of at most 8 stations at a time.

### Changed (exit codes and flags)

These were accepted before and are now refused with exit 3
(`invalid_input`) before anything is sent:

- a negative value of any duration flag, and 0 for `--connect-timeout`,
  `--activity-timeout`, `io statistics --duration` and
  `io monitor --interval` (0 ran `io statistics` until interrupted);
- `--send-clock`, `--reduction-ratio`, `--watchdog-factor` or
  `--data-hold-factor` of 0 (silently replaced by the default before), a
  send clock × reduction ratio above 65535, `--rpc-port` outside 1–65535;
- a `--controller-name`, `--peer-check` station or `--link`/`--expect`
  station that is not a valid NameOfStation, a port ID or
  `--mrp-domain-name` above 240 octets, an `--expect` link without a
  station or port;
- `ip set` and `--assign-ip` with an address that is no host address
  (0.0.0.0, loopback, multicast, broadcast, the network or broadcast
  address of the subnet, a /0 prefix) or a gateway outside the subnet;
- `mrp status --ports 0` (above 255 is a usage error, exit 2).

Refused as a usage error (exit 2):

- `disconnect` without `--session-key` (a Release with key 0 was sent);
- two of `discover --station`, `--alias`, `--ip`, `--vendor/--device`
  (only the first was used); `ip set --clear --gateway`;
  `--mrp-domain-name` without `--mrp-role`.

Other changes:

- `io status --settle 0` takes the snapshot at once instead of waiting
  for Ctrl-C.
- `io statistics` interrupted with Ctrl-C exits 130, as
  docs/exitcodes.md states for one-shot commands (it printed the counters
  with exit 0).
- `mrp status` reads each port with its own `--timeout` and lists the
  ports it could not read in `errors` (exit 7) instead of leaving them
  out; a port submodule the device does not have is still left out.
- `connect --reparam`: the re-parameterization is cancelled before the
  AR is released at the end of `--duration`.

### Dependencies

- go-profinet v0.7.0 (see its release notes), Go 1.26 floor, CI on Go
  1.26 and 1.27, shared workflows v3.3.1.

---

## v0.7.2

**Date:** 2026-10-31
**Previous release:** v0.7.1

### Changed

- Policy 1.2: build output in `bin/` and `release/`, a per-package coverage floor, and more tests and CI. No change in behaviour.
- Built on go-profinet v0.6.2.

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
