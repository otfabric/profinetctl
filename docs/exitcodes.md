# Process exit codes

`profinetctl` uses stable exit codes for scripting. A fatal error is rendered once: as one `error:` line on stderr, or as a single JSON `error` object on stdout when `--format json` (or `PROFINETCTL_FORMAT=json`) is active. A usage error follows `--format` as well, even when the rest of the command line cannot be parsed: `{"error": {"kind": "usage", "code": "invalid_input", "message": ...}}`.

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage (`usage`): unknown command or flag, a missing required flag, no device selected (`--station` or `--mac`), both selections, a missing `--confirm` for `reset` or one that names another device |
| 3 | Invalid input (`invalid_input`): a flag value, environment variable, GSDML file or model that does not make sense, an interface that does not exist. Nothing was sent |
| 4 | Transport (`transport`): the interface cannot be opened (missing privileges: `permission_denied`), a socket cannot be bound or fails |
| 5 | Timeout (`timeout`): the device did not answer a DCP request or an RPC call in time (`--timeout`), or an AR did not reach Running within `--connect-timeout` |
| 6 | Refused or protocol error (`refused`, `protocol`): the device answered with a PNIO error status, refused a DCP Set with a block error, rejected the Connect, or answered with a malformed frame; more than one device answers to the station name; a device has no IP address for RPC |
| 7 | Partial success: a **valid** result was written to stdout, and some of it failed: `gsdml validate` found issues, an I&M record or part of `device info`, `status` or `topology` could not be read |
| 8 | Not found (`not_found`): no device answers to the station name (DCP Identify) |
| 10 | Output or internal (`output`, `internal`): the result could not be written, or something unexpected happened |
| 130 | Interrupted (`interrupted`): Ctrl-C or SIGTERM before a one-shot command had its result |

**Kind vs code:** `kind` drives the exit code; `code` in the JSON error object is stable for machines. Do not parse the human `message`.

| `kind` | `code` values |
|--------|---------------|
| `usage` | `invalid_input`, `invalid_flag_combination`, `confirmation_required` |
| `invalid_input` | `invalid_input`, `invalid_format`, `invalid_flag_combination`, `invalid_gsdml`, `invalid_model`, `input_file_open_failed`, `output_file_create_failed`, `interface_open_failed` (no such interface) |
| `transport` | `interface_open_failed`, `permission_denied`, `socket_failed` |
| `timeout` | `request_timeout`, `connect_failed` |
| `refused` | `pnio_error` (the message names the status: ErrorCode, ErrorDecode, ErrorCode1, ErrorCode2), `dcp_block_error`, `connect_failed`, `unsupported`, `duplicate_station_name`, `invalid_input` (no IP address) |
| `protocol` | `protocol_violation` |
| `not_found` | `device_not_found`, `invalid_input` (`gsdml submodules --module` with an unknown ident number) |
| `interrupted` | `context_canceled` |
| `output` | `table_render_failed`, `output_write_failed`, `output_file_write_failed` |
| `internal` | `internal_unexpected` |

**Partial exit:** only after a success payload was written. A failure to open the interface, to resolve the device or to write is fatal and uses the codes above, never `7`.

**Streaming commands:** `connect`, `alarms` and `io monitor` write one record at a time. `--duration` or Ctrl-C (SIGINT) and SIGTERM end the stream normally with exit code 0: the AR is released, and `connect` writes its summary. A failure before the AR runs is fatal.

**Commands that change a device:** `name set`, `ip set`, `signal`, `reset`, `record write`, `im write`, `disconnect` and the commands that establish an AR validate their flags before sending; a rejected invocation exits `2` or `3` and has sent nothing. A refusal by the device exits `6`, a missing answer `5`; after a timeout a Set may or may not have been applied.

**`--debug`:** logs go-profinet's diagnostics and, after a fatal error, the chain of causes (`cause: …` lines) on stderr. It does not change stdout or the exit code.
