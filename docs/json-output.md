# `--format json` conventions

These conventions are a **stable contract** for scripting. Field names are snake_case. A breaking change to a name or shape is called out in the release notes.

## General

| Concept | JSON fields |
|---------|-------------|
| Device | `mac` (`"00:0e:cf:12:34:56"`), `name_of_station` (or `station` in results about one device), `type_of_station`, `vendor_id`, `device_id` (numbers), `ip`, `mask`, `gateway` (dotted quads, `"0.0.0.0"` when unset) |
| Addresses of a submodule | `api`, `slot`, `subslot`, `index` (numbers) |
| Octets | lowercase hex without separators (`data`, `default`, `signature`, `vendor_data`) |
| Repeated rows | `devices`, `interfaces`, `entries`, `records`, `modules`, `submodules`, `neighbors`, `ports`, `links`, `inputs`, `outputs`: **arrays**, empty rather than null |
| Times in streams | `time`: local time of day with milliseconds (`"14:03:12.120"`) |
| A failure inside a result | `error` (a string) on the entry that failed; `errors` (strings) on results with several parts |
| Fatal error | one object `{"error": {"kind", "code", "message"}}` on stdout, nothing else; see [exitcodes.md](exitcodes.md) |

## Results

| Command | Shape |
|---------|-------|
| `interfaces` | `{"backend", "interfaces": [{"name", "mac", "mtu", "up", "addresses"}]}` |
| `discover`, `devices` | `{"devices": [device], "duplicate_names": [..]}`; a device has `mac`, `name_of_station`, `type_of_station`, `vendor_id`, `device_id`, `instance`, `roles` (`"IO-Device"`, `"IO-Controller"`, `"IO-Multidevice"`, `"PN-Supervisor"`), `ip`, `mask`, `gateway`, `ip_set_by` (`none`, `static`, `dhcp`), `ip_conflict`, `oem_vendor_id`, `oem_device_id`, `options` |
| `identify` | `{"device": device}` |
| `name get` | `{"mac", "name_of_station"}` |
| `ip get` | `{"mac", "ip", "mask", "gateway", "set_by", "ip_conflict"}` |
| `name set`, `ip set`, `signal`, `reset`, `record write`, `im write` | `{"mac", "station", "operation", "value", "permanent"}` |
| `record read` | `{"mac", "station", "api", "slot", "subslot", "index", "length", "data", "blocks"}` |
| `im read` | `{"mac", "station", "slot", "subslot", "records": [{"record": "I&M0", "im0": {...} \| "im1" \| ..., "error"}]}` |
| `diagnostics` | `{"mac", "station", "entries": [{"api", "slot", "subslot", "channel", "direction", "severity", "error_type", "error", "ext_error_type", "ext_add_value", "qualifier", "usi", "vendor_data"}]}` |
| `device info` | `{"device", "im0", "modules": [{"api", "slot", "ident_number", "submodules": [{"subslot", "ident_number"}]}], "errors"}` |
| `status` | `{"device", "dcp", "rpc", "diagnoses": {"fault": n, ...}, "ports": [{"subslot", "port_id", "link", "peer"}], "errors"}` |
| `lldp neighbors` | `{"interface", "neighbors": [{"mac", "chassis_id", "port_id", "ttl", "system_name", "system_description", "port_description", "management_address", "chassis_mac", "alias", "line_delay"}]}` |
| `topology` | `{"host_neighbors", "devices": [{"station", "mac", "ip", "ports": [{"subslot", "port_id", "link", "peers": [{"station", "port", "mac", "line_delay"}]}], "error"}], "links": [{"a", "a_port", "b", "b_port"}]}` |
| `gsdml inspect` | `{"file", "schema_version", "vendor_id", "device_id", "vendor_name", "family", "device_access_points": [...], "valid", "issues"}` |
| `gsdml validate` | `{"file", "valid", "issues": [{"path", "message", "severity", "rule"}], "scope"}`; `severity` is `"error"` (validate reports errors only), `rule` names the rule broken: `"structure"` (or absent), `"reference"` or `"io-data-length"` (input or output data too long for an RT frame) |
| `gsdml check` | `{"file", "schema_version", "errors", "warnings", "unvalidated", "issues": [{"path", "message", "severity", "rule"}], "scope"}`; `severity` is `"error"`, `"warning"` or `"unvalidated"` |
| `gsdml gsdx` | `{"file", "package": {"parts": [{"name", "contentType", "compressedSize", "uncompressedSize", ...}], "relationships", "coreProperties", "signatures": [{"part", "partReferences", "certificates", "verified": false, "reason"}], "issues"}, "gsdml": [{"name", "errors", "warnings", "unvalidated", "error"}], "scope"}` |
| `mrp status` | `{"station", "mac", "role", "domain_name", "domain_uuid", "ring_state", "ring_ports": [{"subslot", "domain_uuid"}]}`; `role` is `"client"`, `"manager"` or `"off"`, `ring_state` (`"open"`, `"closed"`) only for a manager |
| `topology check` | `{"ok", "report": {"links": [{"link": {"a": {"station", "port"}, "b": {...}}, "verdict", "detail"}], "unexpected", "unreachable"}}`; `verdict` is `"ok"`, `"missing"`, `"wrong peer"`, `"unknown port"` or `"unverified"` |
| `gsdml modules`, `gsdml submodules` | `{"dap", "modules": [...]}`, `{"dap", "submodules": [...]}` |
| `ar` | `{"station", "mac", "ip", "ar": {"ar_uuid", "session_key", "ar_type", "rt_class", "advanced", "device_address", "local_alarm_ref", "remote_alarm_ref", "input": iocr, "output": iocr}, "module_diff", "differences"}`; an iocr has `frame_id`, `data_length`, `send_clock_factor`, `reduction_ratio`, `watchdog_factor`, `data_hold_factor`, `cycle_ms` |
| `iocr` | `{"ar_uuid", "entries": [{"iocr", "frame_id", "kind": "data" \| "iocs", "slot", "subslot", "offset", "length"}]}` |
| `io status` | `{"station", "state", "inputs": [{"slot", "subslot", "data", "iops"}], "outputs": [{"slot", "subslot", "iocs"}]}`; a status is `"good"`, `"bad (0x..)"` or `"-"` (not received yet) |
| `io statistics` | `{"station", "state", "duration", "cycle", "stats": {...}, "frames_per_second"}` |

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
