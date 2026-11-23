# Releasing profinetctl

Releases are cut by the **Release** workflow (`.github/workflows/release.yml`, the shared `otfabric/.github` `go-binary-release.yml@v3.8.0`): run it from the Actions tab with the version bump (`patch`, `minor`, `major`). It tags the version, builds the binaries for Linux (x86-64, ARM64, ARMv7), macOS (x86-64, ARM64) and Windows (x86-64, ARM64) with the version, tag, commit and build date in `profinetctl version`, and publishes them with SHA-256 checksums.

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

## v0.18.0

**Date:** 2026-11-23
**Previous release:** v0.17.1

## Summary

Built on go-profinet v0.21.0 (from v0.17.0). `device info` shows the
submodules of every API of a device, not those of API 0 alone; commands
that take `--gsdml` keep the submodules' APIs of the file and send them on
the wire; `gsdml validate` and `gsdml check` report the parameter records
of the file as go-profinet's record rules check them.

## Changes

### Added

- **`device info`**: JSON field `api_discovery`, `"apidata"` when the
  device listed its APIs in APIData (0xF821), `"unavailable"` when it did
  not answer it (docs/json-output.md). The text shows the API of a module
  of an API other than 0 (`slot 4   module 0x00000050 API 0x00004E01`),
  and the table gets an `API` column when such a module is present.
- **`gsdml submodules`**: JSON field `api`, the submodule's
  `VirtualSubmoduleItem` `API` (0 when the file gives none). The text shows
  the API of a submodule of an API other than 0
  (`0x00000050/0x00000052  API 0x00004E01  subslot 0x0002`), and the table
  gets an `API` column when such a submodule is listed.

### Changed

- **`device info`** reads APIData (0xF821) for the device's APIs, then
  RealIdentificationData (0xF000) of each, at most 64, outside an AR;
  `modules` is in slot order, API order within a slot. A failed read of
  one API is an error for that API (`RealIdentificationData (API
  0x00004E01): …`), and the others are shown. A device of API 0 alone is
  shown as before.
- **`--gsdml`** (`connect`, `ar`, `gsdml plan` and the other commands that
  build a model from a GSDML file): the model keeps each submodule's
  `VirtualSubmoduleItem` `API` (go-profinet v0.20.0), so an AR addresses
  those submodules, their IO data and their parameter records in their
  API. `--model FILE` accepts a submodule's `api`.
- **`gsdml generate`** writes the `API` attribute of a submodule of an API
  other than 0.
- **`gsdml validate`** reports the errors of the parameter records:
  a record go-profinet cannot convert (`parameter-record`), Consts that
  share octets, a ValueItemTarget that names no ValueItem, TransferSequence
  values that are not 1 to N. A file with them is no longer valid (exit
  code 7), where before it was valid and failed only with `--gsdml`.
- **`gsdml check`** reports the record rules too (errors, warnings and
  unvalidated issues, e.g. `parameter-encoding` for a field without a
  verified encoding). A record go-profinet cannot convert is now a
  `parameter-record` error, and the device access points are then not
  converted (rule `model`), as with any other error.
- `scope` of both names the record rules (docs/json-output.md).

### Breaking

Consumer assessment: scripts that read the exit code of `device info`, or
that connect to devices described by GSDML files with submodules of an API
other than 0, see a difference; for devices and files of API 0 alone the
output, exit codes and wire traffic are those of v0.17.1, apart from the
new JSON fields (`api_discovery` of `device info`, `api` of
`gsdml submodules`) and the `gsdml validate`/`check` record rules above.

- **Exit code of `device info`**: a device that does not answer APIData
  (0xF821) is read at API 0 only and the command exits **7** (partial),
  with the error "API discovery unavailable; results may be incomplete
  (API 0 only)"; v0.17.1 exited 0 for the same device and did not say the
  result could be incomplete (docs/exitcodes.md).
- **Wire**: with `--gsdml`, a submodule of an API other than 0 is sent in
  its API (Connect, IO data, parameter writes); v0.17.1 sent API 0, which
  such a device refuses or answers for another submodule.
- **GSDML input**: a `VirtualSubmoduleItem` whose `API` is not an
  Unsigned32 is now an invalid GSDML file for `--gsdml` (exit 3,
  `invalid_gsdml`), and a DAP submodule of an API other than 0 is
  unsupported (exit 3, `unsupported_gsdml`); `gsdml check` reports them as
  a `model` error and a `model` unvalidated issue, `gsdml inspect` in the
  device access point's `error`. v0.17.1 ignored the attribute. `--model FILE` with a DAP submodule of an API other than
  0 is an invalid model (exit 3).

---

## v0.17.1

**Date:** 2026-11-22
**Previous release:** v0.17.0

## Summary

`gsdml plan` previews what an AR writes more faithfully: it takes the AR
commands' `--slot`, and its table output no longer hides the plan's
issues.

## Changes

### Added

- **`gsdml plan --slot N`**: only the DAP and these slots, as the AR
  commands' `--slot` (shared device), so that the plan is the one an AR
  with the same flags writes.

### Changed

- **`gsdml plan --format table`** writes the plan's issues to stderr, each
  with its severity; the table itself still has the writes only. Text
  output names the severity of each issue too, and so do the AR commands'
  `warning: parameter plan:` lines.
- An AR command with both a parameter plan (`--param-value`,
  `--param-defaults`) and `--param` warns on stderr that the `--param`
  records are written after the plan's, unchecked against the GSDML file
  and its TransferSequence.

---

## v0.17.0

**Date:** 2026-11-22
**Previous release:** v0.16.0

## Summary

Parameter records from the GSDML file's fields: `--param-value` sets a
field by its ID or name and profinetctl builds the record with go-profinet
v0.17.0's `PlanParameters`, checked against the file before anything is
sent; `gsdml plan` shows the records without connecting.

## Changes

### Added

- **`gsdml plan FILE`**: the parameter records an AR with the same
  `--dap`, `--plug`, `--param-value` and `--param-defaults` writes before
  PrmEnd, in write order (ascending TransferSequence per submodule, then
  the records without one in file order; submodules in slot and subslot
  order), with the records' issues. Sends nothing. Exit code 7 when
  go-profinet refuses the plan for errors of the records (`planned`
  false).
- **`--param-value SLOT:SUBSLOT:INDEX:FIELD=VALUE`** and
  **`--param-defaults`** on `connect`, `ar`, `iocr`, `io` and `alarms`
  (with `--gsdml`): the planned records are written before PrmEnd, ahead
  of `--param`. Only Changeable fields can be set; a value outside a
  field's AllowedValues or ValueItem exits 3 and sends nothing. Warnings of
  the plan go to stderr.

### Changed

- Built on go-profinet v0.17.0 (from v0.14.1).
- Exit code 2 (`invalid_flag_combination`) for `--param-value` or
  `--param-defaults` without `--gsdml`, and for a `--param` of a record
  the plan writes; exit code 3 (`invalid_input`) for a `--param-value` the
  file does not allow (docs/exitcodes.md).

---

## v0.16.0

**Date:** 2026-11-22
**Previous release:** v0.15.1

## Summary

Three places where profinetctl said less than it checked, or more than it
should accept: `gsdml inspect` now gives the validity `gsdml validate`
gives, `gsdml check` reports the device access points it does not convert,
and `--plug` keeps to the slots the GSDML file allows a module in.

### Breaking

- **`gsdml inspect`**: `Valid` (JSON `valid`, `issues`) is what `gsdml
  validate` reports for the file: its structure against the GSDML schema of
  its version, and the semantic rules that are errors.
  - Before, inspect checked go-profinet's version-independent catalog, so
    it could say "yes" for a file that `gsdml validate` refuses.
  - `--against catalog` gives the old result. A new `Checked:` line (JSON
    `scope`) says what was checked.
- **`--plug`** (`connect`, `ar`, `iocr`, `io`, `alarms` with `--gsdml`):
  a module is refused with `invalid_input`, exit code 3, before anything is
  sent, in a slot that is
  - not among the device access point's `PhysicalSlots`, or
  - not in the module's `AllowedInSlots`.
  - A list that is absent or does not parse is not checked. These are the
    lists of go-profinet's slot rule (`gsdml check`, rule `slots`).
  - `FixedInSlots` is not enforced: `--plug` still replaces a module the
    file fixes.
  - `--plug-any-slot` restores the old behaviour, to test how a device
    answers a module in the wrong slot. With `--model` nothing is checked,
    as before.

### Fixed

- **`gsdml check`**: a file with more than 64 device access points has one
  `unvalidated` issue, rule `model`, path `…/DeviceAccessPointList`, for
  those after the first 64. Before, they were not converted to a model, and
  nothing said so. With `--strict` it gives exit code 7.

---

## v0.15.1

**Date:** 2026-11-22
**Previous release:** v0.15.0

## Summary

Built on go-profinet v0.14.1. The file `gsdml generate` writes for a model
with a parameter record without a default value now passes `gsdml
validate` and `gsdml check`.

## Changes

### Fixed

- **`gsdml generate`**: a parameter record without a default value is
  written with a `Const` of zeros.
  - Before, it had no `Const`, which the schemas require from V2.31.
    `gsdml validate` and `gsdml check` reported it as a schema error,
    the known issue of v0.15.0.
  - The cause was go-profinet's `gsdml.FromModel`, fixed in v0.14.1.
  - The record's value is unchanged: a missing default means zeros.
- **`gsdml generate`**: a model with a parameter record of no octets now
  fails for V2.31 and later, because those schemas have no valid form for
  it. The error code is `invalid_model` and the exit code 3.

---

## v0.15.0

**Date:** 2026-11-22
**Previous release:** v0.14.0

Built on go-profinet v0.14.0 (was v0.12.0), which has a generated model of
every GSDML schema version, 1.0 to 2.46, with a validator checked against
xmllint. `gsdml validate` and `gsdml check` now validate a file against the
schema of its own version, and the new `gsdml schema` commands give access
to the model.

### Breaking

- `gsdml validate` and `gsdml check` validate the structure of a file
  against the GSDML schema of the version it names (rule `schema`), not
  go-profinet's version-independent catalog. A file the catalog accepted
  can now have errors, and exit code 7: an element or attribute the
  schema of its version does not allow (an element outside the catalog
  was unvalidated, it is now an error when the schema refuses it), the
  order and number of elements, facets, and identity constraints in their
  scopes. `--against catalog` restores the previous behaviour, and a file
  naming no version go-profinet has a schema for is still checked against
  the catalog (`check` reports that as unvalidated, rule `schema`).
  Consumers of the JSON: the `scope` text changes; `rule` values gain
  `schema`.

### Added

- Issues of `gsdml validate` and `gsdml check` have the `line` of their
  element (JSON `line`, a `line N:` prefix in text, a `LINE` column in
  tables); `gsdml validate` has `schema_version`.
- `gsdml schema versions`: the GSDML schema versions go-profinet has.
- `gsdml schema validate FILE [--version V]`: validation against the
  schema of the file's version (or `V`) only, each issue with its line
  and path; exit code 7 when the file is not valid, `unsupported_gsdml`
  for a file naming no version go-profinet has.
- `gsdml schema dump FILE [--version V] [--no-validate]`: the typed model
  of a file as JSON, in the schema's names and order, with JSON numbers
  and booleans for numeric and boolean types; a float value JSON cannot
  represent (INF, -INF, NaN) is `unsupported_gsdml`.


### Known issue

- A file `gsdml generate` writes for a model with a parameter record
  without a default value (go-profinet v0.14.0 `gsdml.FromModel`) has no
  `Const` element in that `ParameterRecordDataItem`, which the schemas
  require from V2.31 (the file names V2.35): `gsdml validate` and `gsdml check` now report it as an
  error (rule `schema`), as xmllint would. The fix belongs in
  go-profinet; `--against catalog` reports no error.
---

## v0.14.0

**Date:** 2026-11-22
**Previous release:** v0.13.1

Built on go-profinet v0.12.0 (was v0.10.0), whose GSDML conversion refuses
values it used to turn into a default or write wrongly; `gsdml check`
now shows those, and a file go-profinet does not support has its own
error code.

### Added

- `gsdml check` converts each device access point to a model, as
  `--gsdml FILE --dap ID` does, when the catalog finds no errors. One
  that does not convert is an issue with rule `model`: an error (exit
  code 7), or unvalidated when go-profinet does not support what the
  file has, such as a Ref DefaultValue of type `Integer16_S` (exit code 7
  with `--strict`). Until now a file could pass `gsdml check` and still
  fail with `--gsdml`.
- Error code `unsupported_gsdml` (kind `invalid_input`, exit code 3): a
  GSDML file that go-profinet refuses as unsupported rather than
  malformed, a document type declaration with an internal subset or a Ref
  default of a DataType whose encoding it does not know. Those failed as
  `invalid_gsdml` (docs/exitcodes.md).

### Changed

- Built on go-profinet v0.12.0. Its GSDML conversion (`--gsdml`, `gsdml
  inspect`, `modules`, `submodules`) refuses, with `invalid_gsdml` unless
  said: an integer Ref default in Go literal syntax (`"010"` is now ten,
  `"0b1"` refused); a Boolean capability or Ref default other than true,
  false, 1 or 0; a Ref BitLength or BitOffset or a Boolean capability
  given as `""`; malformed optional
  numbers and value lists (`MinDeviceInterval="32ms"`); a string or octet
  default longer than its Length; and, as `unsupported_gsdml`, the cases
  above. Each used to load with a default or a wrong record. From
  go-profinet v0.11.0: a Ref `BitArea` without BitLength loads (BitLength
  1), and the `module_diff` of `ar` no longer lists the matching
  submodules of a go-profinet device.
- `gsdml check`'s `scope` names the conversion of the device access points.

---

## v0.13.1

**Date:** 2026-11-15
**Previous release:** v0.13.0

### Changed

- Built on go-profinet v0.10.0 (was v0.9.1). Its DCP responder answers Get and Set from every requester; a Set inside another requester's open Set transaction is answered with a resource error instead of being ignored. profinetctl does not run a DCP responder, so its commands behave as before.

---

## v0.13.0

**Date:** 2026-11-15
**Previous release:** v0.12.0

### Summary

Two exit-code changes for scripts: `im write` reports a partial write, and
`discover`, `topology` and the lookups by station name handle more than 4096
answers to one DCP Identify. No command or flag is added or removed.

### Breaking

- `im write` with several records, when a record fails after others were
  written: the exit code is 7 (partial) where it was that of the failure (5
  or 6), and stdout has the result where it had the fatal error. The JSON
  result names the records written (`written`, present also when every
  record was written) and the one that failed (`failed`: `record`, `kind`,
  `code`, `message`); text output ends with a `failed:` line, and stderr
  still names the records written. When the first record fails, nothing was
  written and the exit code is that of its error, as before; an interruption
  stays 130. No flag restores the old exit code. `failed.kind` and
  `failed.code` are those of the former fatal error
  ([docs/exitcodes.md](docs/exitcodes.md) maps the kind to its exit code);
  one record per invocation (`--function` and `--location` are one, I&M1)
  keeps the exit code of the failure.
- More than 4096 devices answering one DCP Identify (go-profinet keeps
  4096): `discover` (`devices`) and `topology` show those of the first 4096
  answers, with `"truncated": true` in JSON and a `truncated:` line in text,
  and exit 7. A command that looks a device up with `--station` exits 6
  (`duplicate_station_name`) and sends nothing more. Both printed nothing
  and exited 10 (`internal_unexpected`). No flag restores that: a script
  that handled exit 10 there handles 7 with `truncated`, or 6, instead.

### Documentation

- [docs/exitcodes.md](docs/exitcodes.md) and
  [docs/json-output.md](docs/json-output.md): the two cases above
  (`written`, `failed`, `truncated`). README.md and SECURITY.md: the same.

---

## v0.12.0

**Date:** 2026-11-15
**Previous release:** v0.11.1

### Summary

Fixes in how profinetctl selects a station by name, keeps the I&M1 field not
given to `im write`, bounds the reads of `topology` and the work of
`gsdml inspect`, and ends on Ctrl-C. One flag is added
(`topology --read-any-address`). Exit codes change where a command used to
go on or succeed wrongly, as listed below.

### Fixed

- `--station`, in every command that looks a station up by name and in
  `discover --station`: only an answer that carries the station name is
  taken. An answer with another name counts as no answer: the command exits
  11 (`device_not_found`, the message counts the answers ignored) and sends
  that station nothing. When the named station answers as well, it is the
  one selected, where the exit code was 6 (`duplicate_station_name`).
- `im write` with only `--function` or only `--location` keeps the other
  field of I&M1, as its help says: when the current I&M1 cannot be read (no
  answer, a refusal, a malformed record), it writes nothing and exits with
  the error of the read (5 or 6), where it wrote I&M1 with the other field
  empty and exited 0. When one of several records fails, stderr names those
  written before it; the exit code stays that of the failure.
- `topology` reads only the devices whose IP address lies in a subnet of the
  interface; the others are listed with an error (exit code 7), and the new
  flag `--read-any-address` reads them as before. The reads end after
  2 × `--timeout` in all; the devices not read by then have an error (exit
  code 7).
- `gsdml inspect` converts the first 64 device access points of a file; the
  others are listed with their ID, ident number and conformance class, and
  `"error": "not converted: more than 64 device access points"`.
- The `gsdml` commands exit 130 after Ctrl-C or SIGTERM instead of finishing
  their work and exiting 0 or 7. A second Ctrl-C or SIGTERM ends any command
  at once ([docs/exitcodes.md](docs/exitcodes.md)).

### Documentation

- [docs/json-output.md](docs/json-output.md): `mrp status` of a device
  without media redundancy exits 8, as the code and docs/exitcodes.md say
  (it said 6).
- SECURITY.md: `record write` and `im write` with `--gsdml` or `--model`
  establish an IOCAR; `topology check` binds UDP 34964 and listens for DCP
  Hello; the `--station` and `topology` rules above. README.md:
  `topology check` binds UDP 34964; a missing privilege exits 9, as
  docs/exitcodes.md says (it said 4). RELEASE.md: the release workflow is
  `go-binary-release.yml@v3.8.0`.

### CI

- A job runs `make e2e`, the suite of the built binary, with a case that
  interrupts a command with SIGINT and with SIGTERM and expects exit code
  130.

---

## v0.11.1

**Date:** 2026-11-15
**Previous release:** v0.11.0

No command or flag is added or changed.

### Changed

- Built on go-profinet v0.9.1 (was v0.9.0) (go-profinet RELEASE.md, v0.9.1). For the tool this means:
  - `discover`, `topology` and the commands that look a station up by name end with an error when more than 4096 stations answer one DCP Identify (go-profinet's bound per Identify); below that nothing changes.
  - The GSDML commands (`gsdml check`, `validate`, `inspect`, `modules`, `submodules`, `gsdx`, `generate`) refuse a document of more than 1 048 576 elements, list at most 16 384 issues per severity and one that counts the rest, and refuse a module with more than 1440 submodules or more than 16 MiB of parameter records over its plugged submodules.
  - Controller commands report no AR for a device until its connection is complete; the simulated device ends an AR whose abort arrives while its Connect runs, and refuses that Connect.
  - The IP stack the commands use keeps at most 1024 addresses in its ARP table and closes its sockets when it stops.

---

## v0.11.0

**Date:** 2026-11-14
**Previous release:** v0.10.1

### Summary

profinetctl is built on go-profinet v0.9.0, which bounds the work a peer or
a file can cause and tightens connection lifecycles. The changes reach every
command; the CLI's flags, exit codes and JSON fields are unchanged.

### Changed

- Controller commands ignore RPC answers that come from a host other than the
  one addressed.
- `gsdml` commands bound the work spent on large or repetitive files:
  `gsdml check` reports slot warnings up to a per-list limit and quotes long
  names shortened, and GSDX signature parts are read once.

### Dependencies

- go-profinet v0.8.1 → v0.9.0.

---

## v0.10.1

**Date:** 2026-11-12
**Previous release:** v0.10.0

### Summary

profinetctl is built on go-profinet v0.8.1, whose interop suite runs
profinet-interop v0.8.0. No change in behaviour.

### Dependencies

- go-profinet v0.8.0 → v0.8.1.

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
