# `--format json` conventions

These conventions are a **stable contract** for scripting. Field names are snake_case. A breaking change to a name or shape is called out in the release notes.

## General

| Concept | JSON fields |
|---------|-------------|
| Device | `mac` (`"00:0e:cf:12:34:56"`), `name_of_station` (or `station` in results about one device), `type_of_station`, `vendor_id`, `device_id` (numbers), `ip`, `mask`, `gateway` (dotted quads, `"0.0.0.0"` when unset) |
| Addresses of a submodule | `api`, `slot`, `subslot`, `index` (numbers) |
| Octets | lowercase hex without separators (`data`, `default`, `signature`, `vendor_data`) |
| Repeated rows | `devices`, `interfaces`, `entries`, `modules`, `submodules`, `neighbors`, `ports`, `links`, `inputs`, `outputs`: **arrays**, empty rather than null. Optional lists (`duplicate_names`, `options`, `module_diff`, `differences`, `blocks`, the `records` of a GSDML submodule) and optional values are **omitted** when empty |
| Times in streams | `time`: local time of day with milliseconds (`"14:03:12.120"`) |
| A failure inside a result | `error` (a string) on the entry that failed; `errors` (strings) on results with several parts; `failed` of `im write`: the record and its error (`kind`, `code`, `message`) |
| Fatal error | one object `{"error": {"kind", "code", "message"}}` on stdout, nothing else; see [exitcodes.md](exitcodes.md) |

## Results

| Command | Shape |
|---------|-------|
| `interfaces` | `{"backend", "interfaces": [{"name", "mac", "mtu", "up", "addresses"}]}` |
| `discover`, `devices` | `{"devices": [device], "duplicate_names": [..], "truncated"}`; `truncated` (`true`, omitted otherwise) says that more than 4096 devices answered the DCP Identify: `devices` holds those of the first 4096 answers, and the exit code is 7. A device has `mac`, `name_of_station`, `type_of_station`, `vendor_id`, `device_id`, `instance`, `roles` (`"IO-Device"`, `"IO-Controller"`, `"IO-Multidevice"`, `"PN-Supervisor"`), `ip`, `mask`, `gateway`, `ip_set_by` (`none`, `static`, `dhcp`), `ip_conflict`, `oem_vendor_id`, `oem_device_id`, `options` |
| `identify` | `{"device": device}` |
| `name get` | `{"mac", "name_of_station"}` |
| `ip get` | `{"mac", "ip", "mask", "gateway", "set_by", "ip_conflict"}` |
| `name set`, `ip set`, `signal`, `reset`, `record write` | `{"mac", "station", "operation", "value", "permanent"}` |
| `im write` | `{"mac", "station", "operation", "value", "written": [..], "failed": {"record", "kind", "code", "message"}}`; `written` lists the records written, in order (`"I&M1"` to `"I&M4"`), and `value` joins them; `failed` (omitted when every record was written) is the record that failed after those, with its error as the fatal error object has it, and the exit code is 7. When the first record fails, nothing was written: the result is a fatal error |
| `record read` | `{"mac", "station", "api", "slot", "subslot", "index", "length", "data", "blocks"}` |
| `im read` | `{"mac", "station", "api", "slot", "subslot", "records": [{"record": "I&M0", "im0": {...} \| "im1" \| ..., "error"}]}` |
| `diagnostics` | `{"mac", "station", "entries": [{"api", "slot", "subslot", "channel", "direction", "severity", "error_type", "error", "ext_error_type", "ext_add_value", "qualifier", "usi", "vendor_data"}]}`; without `--slot`/`--subslot` the entries of every API of the device, with `--api`, `--slot`, `--subslot` those of that submodule |
| `device info` | `{"device", "im0", "modules": [{"api", "slot", "module_ident_number", "submodules": [{"subslot", "submodule_ident_number"}]}], "api_discovery", "errors"}`; `modules` holds the RealIdentificationData (0xF000) of every API the device lists in APIData (0xF821), at most 64, in slot order and API order within a slot; `api_discovery` is `"apidata"` when the device listed its APIs and `"unavailable"` when it did not answer APIData, so that `modules` holds API 0 only and `errors` says the result may be incomplete (exit 7) |
| `status` | `{"device", "dcp", "rpc", "diagnoses": {"fault": n, ...}, "ports": [{"subslot", "port_id", "link", "peer"}], "errors"}` |
| `lldp neighbors` | `{"interface", "neighbors": [{"mac", "chassis_id", "port_id", "ttl", "system_name", "system_description", "port_description", "management_address", "management_if_index", "management_oid", "chassis_mac", "alias", "line_delay"}]}` |
| `topology` | `{"host_neighbors", "devices": [{"station", "mac", "ip", "ports": [{"subslot", "port_id", "link", "peers": [{"station", "port", "mac", "line_delay"}]}], "error"}], "links": [{"a", "a_port", "b", "b_port"}], "truncated"}`; `truncated` as for `discover`: `devices` holds those of the first 4096 answers (exit 7) |
| `gsdml inspect` | `{"file", "schema_version", "vendor_id", "device_id", "vendor_name", "family", "device_access_points": [...], "valid", "issues", "scope"}`; `valid` and `issues` (a count) are what `gsdml validate` with the same `--against` reports, and `scope` is its scope; the first 64 device access points are converted for their name, ports, slots and modules, the others have `"error": "not converted: more than 64 device access points"` |
| `gsdml validate` | `{"file", "schema_version", "valid", "issues": [{"path", "line", "message", "severity", "rule"}], "scope"}`; `severity` is `"error"` (validate reports errors only), `rule` names the rule broken: `"schema"` (the schema of the file's version; `line` is that of the element), with `--against catalog` or for a file naming no version with a schema `"structure"` (or absent) or `"reference"`, `"io-data-length"` (input or output data too long for an RT frame), and the parameter record rules of go-profinet: `"parameter-record"` (a record go-profinet cannot convert), `"parameter-layout"`, `"parameter-overlap"`, `"parameter-default"`, `"parameter-allowed-values"` and `"transfer-sequence"`; `line` is absent when unknown; `scope` names the schema version or the catalog |
| `gsdml check` | `{"file", "schema_version", "errors", "warnings", "unvalidated", "issues": [{"path", "line", "message", "severity", "rule"}], "scope"}`; the structure is checked against the schema of the file's version (rule `"schema"`, with `line`), or with `--against catalog` against the catalog; a file naming no version with a schema is checked against the catalog with an `"unvalidated"` issue of rule `"schema"` that says so; `severity` is `"error"`, `"warning"` or `"unvalidated"`; without errors of the catalog or of go-profinet's semantic rules (a record go-profinet cannot convert is a `"parameter-record"` error of those), each of the first 64 device access points that does not convert to a model (as `--gsdml` with its ID would fail) is an issue with `rule` `"model"` and the path `…/DeviceAccessPointItem[ID]`, `"unvalidated"` when go-profinet does not support what the file has, otherwise `"error"`; when there are more than 64, the others are one `"unvalidated"` issue with `rule` `"model"` and the path `…/DeviceAccessPointList` |
| `gsdml gsdx` | `{"file", "package": {"parts": [{"name", "contentType", "compressedSize", "uncompressedSize", ...}], "relationships", "coreProperties", "signatures": [{"part", "partReferences", "certificates", "verified": false, "reason"}], "issues"}, "gsdml": [{"name", "errors", "warnings", "unvalidated", "error"}], "scope"}` |
| `mrp status` | `{"station", "mac", "role", "domain_name", "domain_uuid", "ring_state", "ring_ports": [{"subslot", "domain_uuid"}], "errors"}`; `errors` (omitted when empty) lists the port submodules whose record could not be read (exit 7), a port submodule the device does not have is left out; `role` is `"client"`, `"manager"` or `"off"`, `ring_state` (`"open"`, `"closed"`) only for a manager; a device without media redundancy is an error, `unsupported` (exit 8) |
| `topology check` | `{"ok", "report": {"links": [{"link": {"a": {"station", "port"}, "b": {...}}, "verdict", "detail"}], "unexpected", "unreachable"}}`; `verdict` is `"ok"`, `"missing"`, `"wrong peer"`, `"unknown port"` or `"unverified"` |
| `gsdml generate` | `{"file", "bytes"}` |
| `gsdml schema versions` | `{"versions": ["1.0", …, "2.46"]}`, oldest first |
| `gsdml schema validate` | `{"file", "schema_version", "valid", "issues": [{"line", "path", "message"}], "truncated", "scope"}`; `path` is the element's, from the root (`/ISO15745Profile/…`); `truncated` (present when true): more issues than the first 1000 listed |
| `gsdml schema dump` | `{"file", "schema_version", "validated", "document"}`; `document` is the root element `ISO15745Profile` with the elements and attributes the file has, under the schema's names (absent optional ones are left out), in the schema's order; numeric and boolean types are JSON numbers and booleans, `xsd:integer` and `xsd:decimal` strings; `validated` is false with `--no-validate` |
| `disconnect` | `{"station", "ar_uuid"}` |
| `gsdml plan` | `{"file", "dap", "planned", "writes": [{"api", "slot", "subslot", "index", "name", "module_ident_number", "submodule_ident_number", "transfer_sequence", "values", "length", "data"}], "errors", "warnings", "unvalidated", "issues": [{"path", "line", "message", "severity", "rule"}]}`; `writes` in the order an AR writes them, `api` the API of the write's submodule in the plugged configuration, `data` the record in hex, `values` the number of fields `--param-value` set in it (0: defaults only), `transfer_sequence` 0 when the file gives none; `planned` is false when go-profinet refused the plan for the errors in `issues` (`writes` is then empty, exit code 7); `severity` is `"error"`, `"warning"` or `"unvalidated"` |
| `gsdml modules`, `gsdml submodules` | `{"dap", "modules": [...], "omitted_modules": [...]}`, `{"dap", "submodules": [...]}`; a module has `name`, `ident_number`, `plugged_in_slots`, `submodules`, `input_length`, `output_length`, `info` and `configurable_submodules` (the number of configurable submodules it can choose, absent for none; `submodules` and the lengths are its default configuration's); an omitted module (one the file lets the DAP use that go-profinet cannot place, refused by `--plug`) has `id`, `ident_number`, `reason`, and the key is absent when there is none; a submodule has `module`, `module_ident_number`, `name`, `ident_number`, `api`, `subslot`, `input_length`, `output_length`, `records` |
| `gsdml config` | `{"dap", "slots": [{"slot", "module", "ident_number", "submodules": [{"subslot", "name", "ident_number", "api", "input_length", "output_length", "records", "id"}]}], "choices": [{"slot", "module", "id", "name", "ident_number", "port", "allowed_in_subslots", "fixed_in_subslots", "used_in_subslots", "error"}]}`; slot 0 is the DAP; a submodule's `id` is the SubmoduleItem of a configurable submodule (absent for a built-in one, or when two configurable submodules with its ident number allow its subslot); `choices` are the configurable submodules of the DAP and of each plugged module (`module` is the module reference's ID, the DAP's name for slot 0), `error` (absent when none) why one cannot be placed; `port`, the subslot lists and `error` are absent when empty |
| `ar` | `{"station", "mac", "ip", "ar": {"ar_uuid", "session_key", "ar_type", "rt_class", "advanced", "device_address", "local_alarm_ref", "remote_alarm_ref", "input": iocr, "output": iocr}, "module_diff", "differences"}`; an iocr has `frame_id`, `data_length`, `send_clock_factor`, `reduction_ratio`, `watchdog_factor`, `data_hold_factor`, `cycle_ms` |
| `iocr` | `{"ar_uuid", "entries": [{"iocr", "frame_id", "kind": "data" \| "iocs", "slot", "subslot", "offset", "length"}]}` |
| `io status` | `{"station", "state", "inputs": [{"slot", "subslot", "data", "iops"}], "outputs": [{"slot", "subslot", "iocs"}]}`; a status is `"good"`, `"bad (0x..)"` or `"-"` (not received yet) |
| `io statistics` | `{"station", "state", "duration", "cycle", "stats": {...}, "frames_per_second"}` |
| `version` (`--json`, `-o json`) | `name`, `version` (no `v`; `"dev"` for a development build), `tag` (`""` without one), `commit` (`"unknown"` when not known), `built` (RFC 3339, or `"unknown"`), `libraries` (the OT Fabric libraries of the build: `name`, `version`), `go`, `platform` (`"linux/amd64"`), in that order |

## Streams (JSON Lines)

`connect`, `alarms`, `io monitor` and `hello listen` write one object per line, each with `time` and `event`:

| `event` | Fields |
|---------|--------|
| `state` | `from`, `to` (Offline, Resolving, Connecting, Parameterizing, WaitApplicationReady, Running, Stopped), `error` on a failure |
| `alarm` | `alarm`: `type` (Diagnosis, Process, Pull, Plug, ...), `priority` (`high`, `low`), `api`, `slot`, `subslot`, `module_ident_number`, `submodule_ident_number`, `sequence`, `usi`, `data`, `diagnosis` (decoded items of a diagnosis alarm) |
| `module-diff` | `error`: the differences between the real and the expected configuration |
| `reparameterized` | `connect --reparam`: the re-parameterization of the running AR ended; `error` when it failed |
| `sample` | (`io monitor`) `inputs` |
| `hello` | (`hello listen`) `device`: as an entry of `discover` |
| `summary` | (`connect`, last) `state`, `error`, `stats`, `inputs` |
