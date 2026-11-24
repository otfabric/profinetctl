# profinetctl — PROFINET IO Command-Line Tool: Discovery, Names & Addresses, Records, I&M, Diagnosis, GSDML, Topology and Cyclic IO

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI Status](https://github.com/otfabric/profinetctl/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/profinetctl/actions/workflows/ci.yml)
[![Code Coverage](https://codecov.io/gh/otfabric/profinetctl/graph/badge.svg)](https://codecov.io/gh/otfabric/profinetctl)
[![Latest Release](https://img.shields.io/github/v/release/otfabric/profinetctl?label=release)](https://github.com/otfabric/profinetctl/releases)

**profinetctl is a command-line tool for PROFINET IO (IEC 61158-6-10 / IEC 61784-2), the industrial Ethernet of PLCs, remote IO, drives and field devices.** It finds the devices on a network and assigns their station names and IP addresses with DCP, reads their identification (I&M0–4), records and diagnosis over DCE/RPC, inspects, validates and generates GSDML files, lists LLDP neighbors and maps the cabling, and connects to a device as IO-Controller to watch its cyclic IO data and alarms.

It is built for commissioning, troubleshooting and test benches. Output is text, a table or JSON with stable exit codes, so it works in scripts and CI. It is written in Go on top of [go-profinet](https://github.com/otfabric/go-profinet) and runs on Linux, macOS and Windows.

> **Responsible use:** profinetctl sends real PROFINET frames. Only use it on devices and networks you own or are authorized to work on. `name set`, `ip set`, `reset`, `record write`, `im write` and `connect --output` change devices or what they drive; `reset` erases configuration and needs `--confirm`. See [SECURITY.md](SECURITY.md).

**Quick links:** [Install](#install) · [Examples with real output](docs/EXAMPLES.md) · [Quick start](#quick-start) · [Commands](#commands) · [Privileges](#privileges) · [Connecting as IO-Controller](#connecting-as-io-controller) · [Output](#output) · [Limitations](#limitations)

## Install

```bash
curl -fsSL https://get.otfabric.io/profinetctl | sh
```

```powershell
irm https://get.otfabric.io/profinetctl.ps1 | iex
```

Linux (amd64, arm64, armv7), macOS (Intel, Apple silicon) and Windows (amd64, arm64). The installer verifies the release's SHA-256 checksum, installs per user without `sudo` (`~/.local/bin`, Windows `%LOCALAPPDATA%\OTFabric\bin`), and upgrades in place when run again. Pin a version with `OTF_VERSION=x.y.z`; for CI, containers and device images use `OTF_UNMANAGED_INSTALL=<dir>`; all options: [otfabric/install](https://github.com/otfabric/install#options). For offline machines, the archives and checksums are on [Releases](https://github.com/otfabric/profinetctl/releases).

From source (Go 1.26+): `go install github.com/otfabric/profinetctl@latest`

### Shell completion

Commands, flags, the values of `--format`, `--mode`, `--mrp-role` and `im read --record`, the Ethernet interfaces of `--interface`, and GSDML, GSDX, model, expected-link and pcap files complete.

```bash
# bash (needs the bash-completion package)
mkdir -p ~/.local/share/bash-completion/completions
profinetctl completion bash > ~/.local/share/bash-completion/completions/profinetctl

# zsh: a directory on your fpath, then restart the shell
mkdir -p ~/.zsh/completions && profinetctl completion zsh > ~/.zsh/completions/_profinetctl
# and once in ~/.zshrc:  fpath=(~/.zsh/completions $fpath); autoload -U compinit && compinit

# fish
mkdir -p ~/.config/fish/completions && profinetctl completion fish > ~/.config/fish/completions/profinetctl.fish
```

```powershell
# PowerShell: add this line to your profile ($PROFILE)
profinetctl completion powershell | Out-String | Invoke-Expression
```

---

## Quick start

```console
# Which interfaces are there?
profinetctl interfaces

# Which PROFINET devices are on eth0?
sudo profinetctl -i eth0 discover

# Name a new device (it has no name yet: select it by MAC) and give it an address
sudo profinetctl -i eth0 name set io-rack-1 --mac 00:0e:cf:12:34:56 --permanent
sudo profinetctl -i eth0 ip set 192.168.0.10/24 -s io-rack-1 --permanent

# Flash its LED to find it in the cabinet
sudo profinetctl -i eth0 signal -s io-rack-1

# Identification, real configuration, health
sudo profinetctl -i eth0 device info -s io-rack-1
sudo profinetctl -i eth0 im read -s io-rack-1
sudo profinetctl -i eth0 diagnostics -s io-rack-1
sudo profinetctl -i eth0 status -s io-rack-1

# Which device is plugged into which port?
sudo profinetctl -i eth0 topology

# Connect as IO-Controller with the device's GSDML file and watch its inputs
sudo profinetctl -i eth0 io monitor -s io-rack-1 --gsdml GSDML-V2.35-Vendor-Device-20240101.xml
```

```console
$ sudo profinetctl -i eth0 discover
00:0e:cf:12:34:56  io-rack-1                vendor 0x0493 device 0x0002  192.168.0.10  P-Net Sample App
00:0e:cf:12:34:57  drive-3                  vendor 0x002A device 0x0310  192.168.0.13  Drive
2 device(s)
```

## Privileges

DCP, LLDP and the cyclic data are raw Ethernet frames (EtherType 0x8892 and 0x88CC), which ordinary users cannot send:

| Platform | Backend | Needs |
|----------|---------|-------|
| Linux | AF_PACKET | root, or `sudo setcap cap_net_raw,cap_net_admin+ep $(which profinetctl)` |
| macOS | BPF (`/dev/bpf*`) | root, or membership of the `access_bpf` group (Wireshark's ChmodBPF installs it) |
| Windows | [Npcap](https://npcap.com) | Npcap installed (WinPcap API-compatible mode); run as Administrator unless Npcap is restricted to non-admins off |

A missing privilege exits with code 9 and the error code `permission_denied` ([docs/exitcodes.md](docs/exitcodes.md)). `profinetctl interfaces` and the `gsdml` commands need none. Records, I&M and diagnosis use DCE/RPC over the host's UDP stack (port 34964); `connect` and the commands that establish an AR also listen on UDP 34964 for the device's ApplicationReady (`--rpc-port` changes it), and `topology check` binds UDP 34964 for its reads.

## Commands

| Command | What it does |
|---------|--------------|
| `interfaces` | list the Ethernet interfaces |
| `discover`, `devices` | DCP Identify: every device, or those matching `--station`, `--alias`, `--ip`, `--vendor/--device`; duplicate station names are flagged; `--wait` (2s) and `--response-delay` (×10 ms, default 10); above 4096 answers it lists those of the first 4096, marked truncated (exit code 7) |
| `identify` | the full DCP identity of one device |
| `name get`, `name set NAME`, `name set --clear` | read or assign the station name (temporary, or `--permanent`) |
| `ip get`, `ip set ADDR/PREFIX [--gateway GW]`, `ip set --clear` | read or assign the IP suite |
| `signal` | flash the device's LED |
| `reset --mode MODE --confirm TARGET` | DCP ResetToFactory (`communication`, `application`, `engineering`, `all-data`, `device`, `factory`) |
| `record read --index I [--slot --subslot --api] [--max-length N]` | ReadImplicit of any record, as hex, with the block types it holds (`--max-length`: largest record accepted, default 4096) |
| `record write --index I --data HEX \| --data-file FILE` | write a record in a device access AR, or with `--gsdml`/`--model` (and `--dap`) in an IOCAR, for devices without device access |
| `im read [--record 0,1,...] [--api --slot --subslot]` | I&M0, and the I&M1–4 it lists as supported, of the DAP or of the submodule at (API, slot, subslot) |
| `im write --function --location --date --descriptor --signature` | write I&M1–4 of the DAP (in an IOCAR with `--gsdml`/`--model`, as `record write`); when a record fails after others were written, the result names both (exit code 7) |
| `diagnostics [--api --slot --subslot]` | the diagnosis of the device (every API, with the API of an entry of an API other than 0) or of one submodule, decoded |
| `device info` | identity, I&M0 and the real configuration (modules and submodules of every API the device lists in APIData, RealIdentificationData) |
| `status` | reachability (DCP, RPC), diagnosis by severity, link and neighbor of each port |
| `lldp neighbors [--wait D]` | the LLDP neighbors of the host's interface (listens 6 s) |
| `topology [--wait D] [--lldp-wait D] [--read-any-address]` | every device's port neighbors (PDRealData) and the links between them; it reads the devices in a subnet of the interface (with `--read-any-address` all), 8 at a time, for at most 2 × `--timeout`; above 4096 answers to its Identify it maps the first 4096, marked truncated (exit code 7) |
| `topology check --link A/PORT=B/PORT ... \| --expect FILE` | verify the cabling against the expected links: each link ok, missing, wrong peer, unknown port or unverified (exit code 7 when it differs) |
| `mrp status [--ports N]` | a device's media redundancy: role, domain, ring state (manager), ring ports (reads N port submodules, default 4) |
| `hello listen [--duration D]` | stream the DCP Hello frames devices send at start and on link up |
| `gsdml inspect FILE` | identity, schema version, device access points, modules (of the first 64 device access points) |
| `gsdml validate FILE [--against schema\|catalog]` | validation against the GSDML schema of the file's version, 1.0 to 2.46, each issue with its line, and the IO data length (exit code 7 when it finds errors); `--against catalog` (and a file naming no version with a schema) uses go-profinet's version-independent catalog |
| `gsdml gsdx FILE [--extract PART [-O FILE]]` | inspect a GSDX container: parts, relationships, signature metadata (not verified), each GSDML part checked (exit code 7 on issues) |
| `gsdml check FILE [--strict] [--against schema\|catalog]` | the full check: the schema of the file's version as `validate`, errors, warnings and unvalidated content, each with its rule, and whether each device access point converts to a model as `--gsdml` needs (exit code 7 on errors, with `--strict` on any issue) |
| `gsdml schema versions` | the GSDML schema versions go-profinet has (1.0 to 2.46) |
| `gsdml schema validate FILE [--version V]` | validation against the schema of the file's version (or `V`) only: structure, simple types and facets, identity constraints, each issue with its line (exit code 7 when it is not valid) |
| `gsdml schema dump FILE [--version V] [--no-validate]` | the typed model of the file as JSON, with the schema's names and JSON numbers and booleans for numeric and boolean types |
| `gsdml modules FILE`, `gsdml submodules FILE` | the module and submodule types, with IO lengths, records and (`submodules`) the API of a submodule of an API other than 0; `modules` lists the modules with configurable submodules too, with their number and the IO lengths of their defaults, and apart, with why, those go-profinet cannot place |
| `gsdml config FILE [--dap ID] [--plug SLOT=IDENT]... [--plug-submodule SLOT:SUBSLOT=SUBMODULE]...` | the configuration of a device access point, resolved with go-profinet's `gsdml.Configuration`: each slot (0 is the DAP) with its module and submodules, a configurable submodule marked with its SubmoduleItem ID, and the configurable submodules the DAP and each plugged module can put in their subslots (allowed, fixed and used subslots); `--plug` and `--plug-submodule` change it as for an AR; sends nothing |
| `gsdml plan FILE [--dap ID] [--plug SLOT=IDENT]... [--plug-submodule SLOT:SUBSLOT=SUBMODULE]... [--slot N]... [--param-value SLOT:SUBSLOT:INDEX:FIELD=VALUE]... [--param-defaults]` | the parameter records an AR with the same flags writes before PrmEnd, in write order (with the API of a submodule of an API other than 0), built from the file's fields with go-profinet's `PlanParameters`, with the issues of the records and their severity (with `--format table` on stderr); sends nothing (exit code 7 when go-profinet refuses the plan for errors of the records). An AR's `--param` records are not part of the plan |
| `gsdml generate --model MODEL.json [--output-file FILE\|DIR] [--pnio-version V] [--device-access=false]` | the GSDML file of a go-profinet device model (`--output-file`/`-O`: a file, or a directory for the conventional name; `--pnio-version`: declared PNIO_Version, default V2.35; `--device-access`: declare DeviceAccessSupported, default true) |
| `connect` | establish an AR as IO-Controller and run it: states and alarms are streamed, then a summary; `--slot N` expects only some modules (shared device: another controller owns the rest), `--peer-check PORT=STATION/PORT` has the device watch a port's neighbor, `--mrp-role client\|manager\|off` gives a multiport device its MRP role, `--reparam SLOT:SUBSLOT:INDEX:HEX` re-parameterizes the running AR (PrmBegin … PrmEnd) |
| `ar` | establish an AR and show what Connect negotiated (AR, IOCRs, ModuleDiff) |
| `iocr` | the layout of the IOCRs: data objects, IOPS and IOCS at their offsets |
| `io status [--settle D]`, `io monitor [--interval D] [--changes]`, `io statistics [--duration D]` | a snapshot of the inputs and output statuses; the inputs at an interval (only changes with `--changes`); the cyclic counters over 5 s |
| `alarms` | establish an AR and stream the device's alarms |
| `disconnect --ar UUID --session-key N` | release an AR another controller left behind |
| `version [--json]`, `completion SHELL` | the version on one line (or JSON), with the go-profinet version; shell completion for bash, zsh, fish and PowerShell |

Every command has `--help`. A device is selected by `--station NAME` (resolved with DCP Identify, which waits `--wait`, default 1s; several answers are an error) or `--mac`; `--ip` overrides the RPC address it reports.

### Global flags

| Flag | Environment | Default | Meaning |
|------|-------------|---------|---------|
| `-i`, `--interface` | `PROFINETCTL_INTERFACE` | | the Ethernet interface |
| `-o`, `--format` | `PROFINETCTL_FORMAT` (`PROFINETCTL_OUTPUT_FORMAT` stays accepted) | `text` | `text`, `json` or `table` |
| `--timeout` | `PROFINETCTL_TIMEOUT` | `5s` | time limit of one request |
| `--pcap FILE` | | | record every frame sent and received (open it with Wireshark) |
| `--debug` | | | go-profinet's diagnostics on stderr, and the causes of an error |

## Connecting as IO-Controller

`connect`, `ar`, `iocr`, `io` and `alarms` act as IO-Controller (conformance class A, RT_CLASS_1, or RT_CLASS_2 with `--advanced`). They need the expected configuration of the device: its GSDML file (`--gsdml FILE`, with `--dap ID` when it has several device access points), or a go-profinet model in JSON (`--model`). The modules a GSDML file fixes or uses in slots are expected; `--plug SLOT=IDENT` plugs others from its catalog (`gsdml modules` lists them), in a slot the file allows them in (the device access point's `PhysicalSlots` and the module's `AllowedInSlots`); `--plug-any-slot` lifts that, to test how a device answers a module in the wrong slot.

Configurable submodules (a module's or the DAP's `UseableSubmodules`, from the file's `SubmoduleList`) are configured with go-profinet's `gsdml.Configuration`: each is in the subslots the file fixes it in, else uses it in, and `--plug-submodule SLOT:SUBSLOT=SUBMODULE` (repeatable, after `--plug`; slot 0 is the DAP) puts another in a subslot the file allows it in, by its SubmoduleItem ID or by its ident number when only one has it, or with `-` leaves a subslot empty; `gsdml config` shows the configuration and the choices. profinetctl uses the configuration for `--plug-submodule`, for a device access point whose defaults need configurable submodules, and for a `--plug` of a module that has them; otherwise the model is go-profinet's `gsdml.ToModel`, as before. The configuration plugs a module only in a slot the file allows (`--plug-any-slot` is refused with it) and does not replace a module the file fixes in a slot; a subslot the file uses two submodules in by default needs a `--plug-submodule`. Its reading of the subslot attributes is go-profinet's, not checked against PI's specification or an engineering tool.

```console
$ sudo profinetctl -i eth0 connect -s io-rack-1 --gsdml device.xml --plug 3=0x30 \
    --output 2:1:00000001 --param 1:1:123:01020304 --duration 10s
01:12:44.120 state Offline → Resolving
01:12:44.121 state Resolving → Connecting
01:12:44.130 state Connecting → Parameterizing
01:12:44.135 state Parameterizing → WaitApplicationReady
01:12:44.161 state WaitApplicationReady → Running
01:12:50.002 alarm Diagnosis (low) slot 1 subslot 0x0001 seq 0; channel 0x0001 line break fault
01:12:54.161 summary: state Running, 1 connect(s), 0 abort(s), 1 alarm(s), 2500 frames sent, 2500 received, 0 watchdog expiries
```

- The cycle is `--send-clock` (units of 31.25 µs, 32 = 1 ms) × `--reduction-ratio` (default 4 ms); `--watchdog-factor` and `--data-hold-factor` are in cycles (default 10: a general-purpose OS stalls for 10–20 ms now and then, which the 3 cycles PLCs use do not absorb).
- Outputs not given with `--output SLOT:SUBSLOT:HEX` are sent as zeros with IOPS *bad*, so that the device keeps them in their safe state.
- When the device's real configuration differs, the AR still runs (PROFINET reports the difference in the ModuleDiffBlock): profinetctl warns on stderr and streams a `module-diff` event.
- The controller re-establishes the AR after a failure; `connect` streams every state change. Ctrl-C ends it normally: the AR is released and the summary written.
- `--assign-ip ADDR/PREFIX` sets the device's address with DCP before connecting (`--permanent-ip` stores it).
- AR timing and identity: `--connect-timeout` (20s, until Running), `--activity-timeout` (10s, CM initiator activity), `--controller-name` (`profinetctl`).
- `--param-value SLOT:SUBSLOT:INDEX:FIELD=VALUE` (repeatable) sets a field of a parameter record the GSDML file describes, by the field's ID or by a name only one field of the record has, in the lexical form of the file's DefaultValue; the record is built from the file's fields (its other fields at their defaults) and written before PrmEnd. `--param-defaults` writes every record of the plugged submodules, with the file's defaults where no value is given. Only `Changeable` fields can be set, and a value outside the field's `AllowedValues` or `ValueItem` is refused before anything is sent; `gsdml plan` shows the records without connecting. Both need `--gsdml`; a `--param` of a record the plan writes is refused. `--param` records of other records are written after the planned ones, as given: they are not checked against the file or ordered by its TransferSequence, and profinetctl warns on stderr when the two are mixed. Warnings of the plan are written to stderr.
- `--slot N` (repeatable) expects only those modules (shared device), `--peer-check PORT=STATION/PORT` has the device check a port's neighbor, `--mrp-role client|manager|off` (with `--mrp-domain-name`) sets a multiport device's MRP role, and `--reparam SLOT:SUBSLOT:INDEX:HEX` re-parameterizes the running AR after `--reparam-after` (1s).

## Output

`--format text` (default) is for people, `table` aligns columns, `json` is for programs: one JSON document, or JSON Lines for the streaming commands (`connect`, `alarms`, `io monitor`, `hello listen`). JSON field names are snake_case and stable: see [docs/json-output.md](docs/json-output.md). A fatal error is one `error:` line on stderr, or with `--format json` one `{"error": {"kind", "code", "message"}}` object on stdout.

| Exit code | Meaning |
|-----------|---------|
| 0 | success |
| 2 | usage: unknown command or flag, missing selection or confirmation |
| 3 | invalid input: a flag value, GSDML or model file that does not make sense; nothing was sent |
| 4 | transport: the interface cannot be opened, a socket cannot be bound |
| 5 | timeout: the device did not answer |
| 6 | refused or protocol error: a PNIO error status, a DCP block error, a duplicate station name (or more than 4096 answers to a lookup by name), a malformed answer |
| 7 | partial: a result was written and some of it failed (a GSDML file with issues, a topology check that found differences, an I&M record that could not be read or written, a device list cut at 4096) |
| 8 | unsupported: the device does not support the operation |
| 9 | permission: the host denies raw Ethernet access |
| 10 | output or internal error |
| 11 | not found: no device answers to the station name |
| 130 | interrupted (Ctrl-C) before a result |

Details in [docs/exitcodes.md](docs/exitcodes.md).

## PROFINET in two minutes

- **DCP** (Discovery and Configuration Protocol, Layer 2) finds devices (Identify), reads and sets their station name and IP address (Get, Set), flashes their LED and resets them. A controller finds a device by its **station name**; a new device has none and is selected by MAC address.
- **DCE/RPC over UDP 34964** carries the acyclic services: **Connect** establishes an **application relationship (AR)** with the expected configuration, **Read/Write** access records (I&M, diagnosis, parameters), **Control** ends the parameterization. ReadImplicit reads records without an AR.
- **Cyclic RT frames** (Layer 2, EtherType 0x8892) carry the IO data of the two **IOCRs** of an AR, every cycle, each value followed by its provider status (IOPS) and acknowledged by a consumer status (IOCS).
- **Alarms** (diagnosis, process, plug/pull) arrive on the alarm channel and are acknowledged.
- **GSDML** is the XML description of a device: its modules, submodules, IO data and records. A controller's configuration comes from it.
- **LLDP** tells each port who its neighbor is; devices report it in PDPortDataReal, which `topology` reads.

## Limitations

What go-profinet (and so profinetctl) supports and how it is tested against independent implementations is in go-profinet's [support, feature and interop matrix](https://github.com/otfabric/go-profinet/blob/main/docs/FEATURE-STATUS.md).

- IO-Controller with RT_CLASS_1 and RT_CLASS_2 (advanced startup); no IRT (RT_CLASS_3), no PROFIsafe. MRP: it gives a multiport device its role (`--mrp-role`) and reads its state (`mrp status`); it is not an MRP node itself. The automatic manager (MRA) is a role of a device's own configuration (PROFINET's MRP_Role has no value for it), so `--mrp-role` cannot set it; `mrp status` shows what such a device acts as, client or manager. Shared device: it expects a subset of the modules (`--slot`); another controller owns the rest. System redundancy (S2), an experiment of go-profinet that is not interoperable, is not exposed.
- `connect --reparam` needs a device that accepts PrmBegin in Data: go-profinet's device and profinet-py do; p-net and pnio-rs refuse it (the AR runs on, the event reports the refusal).
- OT Fabric is not a PROFIBUS & PROFINET International member: no verification of GSDX signatures (`gsdml gsdx` inspects the container), no MRCD, no certification. The full impact is in go-profinet's [docs/PI-MEMBERSHIP.md](https://github.com/otfabric/go-profinet/blob/main/docs/PI-MEMBERSHIP.md).
- `gsdml validate`, `gsdml check` and `gsdml schema validate` validate against go-profinet's model of the GSDML schemas, generated from PI's reference schemas and checked against xmllint (go-profinet's [docs/GSDML-TYPED.md](https://github.com/otfabric/go-profinet/blob/main/docs/GSDML-TYPED.md)); the schemas are not part of profinetctl, and none of this is PI's GSDML checker.
- `disconnect` cannot release an AR at devices that tie it to the RPC activity of its Connect (p-net); they close it after the CM activity timeout.
- `topology` reads devices that have an IP address in a subnet of the interface (`--read-any-address`: any address); switches without PROFINET (no DCP) appear only as neighbors.
- Timing of the cyclic exchange depends on the host: a general-purpose OS schedules cycles of 1 ms and up; see the jitter in `io statistics`.
- profinetctl is not certified by PROFIBUS & PROFINET International and claims no conformance.

## Development

```bash
make test       # unit and integration tests (race detector)
make coverage   # coverage, fails below 90 %
make check      # everything CI runs
make build      # checks, then bin/profinetctl (make build-nocheck skips the tests)
```

The tests run profinetctl in process against go-profinet IO-Devices on a virtual Ethernet network, so they need neither privileges nor hardware. `make pnet-smoke` runs the DCP, record, I&M, diagnosis, topology and AR commands against an independent implementation, rt-labs p-net in Docker (the images of [profinet-interop](https://github.com/otfabric/profinet-interop)), on a real Linux bridge, and the commands that take `--api` against p-net with its IO module in API 0x4E01 and against go-profinet's interop device (Docker and python3; CI runs it on every pull request that changes code). See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT, see [LICENSE](LICENSE).
