# profinetctl — PROFINET IO Command-Line Tool: Discovery, Names & Addresses, Records, I&M, Diagnosis, GSDML, Topology and Cyclic IO

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI Status](https://github.com/otfabric/profinetctl/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/profinetctl/actions/workflows/ci.yml)
[![Code Coverage](https://codecov.io/gh/otfabric/profinetctl/graph/badge.svg)](https://codecov.io/gh/otfabric/profinetctl)
[![Latest Release](https://img.shields.io/github/v/release/otfabric/profinetctl?label=release)](https://github.com/otfabric/profinetctl/releases)

**profinetctl is a command-line tool for PROFINET IO (IEC 61158-6-10 / IEC 61784-2), the industrial Ethernet of PLCs, remote IO, drives and field devices.** It finds the devices on a network and assigns their station names and IP addresses with DCP, reads their identification (I&M0–4), records and diagnosis over DCE/RPC, inspects, validates and generates GSDML files, lists LLDP neighbors and maps the cabling, and connects to a device as IO-Controller to watch its cyclic IO data and alarms.

It is built for commissioning, troubleshooting and test benches. Output is text, a table or JSON with stable exit codes, so it works in scripts and CI. It is written in Go on top of [go-profinet](https://github.com/otfabric/go-profinet) and runs on Linux, macOS and Windows.

> **Responsible use:** profinetctl sends real PROFINET frames. Only use it on devices and networks you own or are authorized to work on. `name set`, `ip set`, `reset`, `record write`, `im write` and `connect --output` change devices or what they drive; `reset` erases configuration and needs `--confirm`. See [SECURITY.md](SECURITY.md).

**Quick links:** [Examples with real output](docs/EXAMPLES.md) · [Quick start](#quick-start) · [Commands](#commands) · [Privileges](#privileges) · [Connecting as IO-Controller](#connecting-as-io-controller) · [Output](#output) · [Limitations](#limitations)

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

## Installation

Build from source with Go 1.25 or later:

```bash
go install github.com/otfabric/profinetctl@latest
```

or clone and `make build-nocheck` (the binary is `bin/profinetctl`). `make build-all` builds Linux (x86-64, ARM64, ARMv7), macOS (x86-64, ARM64) and Windows (x86-64, ARM64) binaries into `release/`.

## Privileges

DCP, LLDP and the cyclic data are raw Ethernet frames (EtherType 0x8892 and 0x88CC), which ordinary users cannot send:

| Platform | Backend | Needs |
|----------|---------|-------|
| Linux | AF_PACKET | root, or `sudo setcap cap_net_raw,cap_net_admin+ep $(which profinetctl)` |
| macOS | BPF (`/dev/bpf*`) | root, or membership of the `access_bpf` group (Wireshark's ChmodBPF installs it) |
| Windows | [Npcap](https://npcap.com) | Npcap installed (WinPcap API-compatible mode); run as Administrator unless Npcap is restricted to non-admins off |

A missing privilege exits with code 4 and the error code `permission_denied`. `profinetctl interfaces` and the `gsdml` commands need none. Records, I&M and diagnosis use DCE/RPC over the host's UDP stack (port 34964); `connect` and the commands that establish an AR also listen on UDP 34964 for the device's ApplicationReady (`--rpc-port` changes it).

## Commands

| Command | What it does |
|---------|--------------|
| `interfaces` | list the Ethernet interfaces |
| `discover`, `devices` | DCP Identify: every device, or those matching `--station`, `--alias`, `--ip`, `--vendor/--device`; duplicate station names are flagged; `--wait` (2s) and `--response-delay` (×10 ms, default 10) |
| `identify` | the full DCP identity of one device |
| `name get`, `name set NAME`, `name set --clear` | read or assign the station name (temporary, or `--permanent`) |
| `ip get`, `ip set ADDR/PREFIX [--gateway GW]`, `ip set --clear` | read or assign the IP suite |
| `signal` | flash the device's LED |
| `reset --mode MODE --confirm TARGET` | DCP ResetToFactory (`communication`, `application`, `engineering`, `all-data`, `device`, `factory`) |
| `record read --index I [--slot --subslot --api]` | ReadImplicit of any record, as hex, with the block types it holds |
| `record write --index I --data HEX \| --data-file FILE` | write a record in a device access AR, or with `--gsdml`/`--model` (and `--dap`) in an IOCAR, for devices without device access |
| `im read [--record 0,1,...]` | I&M0, and the I&M1–4 it lists as supported |
| `im write --function --location --date --descriptor --signature` | write I&M1–4 of the DAP (in an IOCAR with `--gsdml`/`--model`, as `record write`) |
| `diagnostics [--slot --subslot]` | the diagnosis of the device or of one submodule, decoded |
| `device info` | identity, I&M0 and the real configuration (modules and submodules, RealIdentificationData) |
| `status` | reachability (DCP, RPC), diagnosis by severity, link and neighbor of each port |
| `lldp neighbors [--wait D]` | the LLDP neighbors of the host's interface (listens 6 s) |
| `topology [--wait D] [--lldp-wait D]` | every device's port neighbors (PDRealData) and the links between them |
| `topology check --link A/PORT=B/PORT ... \| --expect FILE` | verify the cabling against the expected links: each link ok, missing, wrong peer, unknown port or unverified (exit code 7 when it differs) |
| `mrp status [--ports N]` | a device's media redundancy: role, domain, ring state (manager), ring ports (reads N port submodules, default 4) |
| `hello listen [--duration D]` | stream the DCP Hello frames devices send at start and on link up |
| `gsdml inspect FILE` | identity, schema version, device access points, modules |
| `gsdml validate FILE` | structural validation (exit code 7 when it finds issues) |
| `gsdml gsdx FILE [--extract PART [-O FILE]]` | inspect a GSDX container: parts, relationships, signature metadata (not verified), each GSDML part checked (exit code 7 on issues) |
| `gsdml check FILE [--strict]` | the full check: errors, warnings and unvalidated content, each with its rule (exit code 7 on errors, with `--strict` on any issue) |
| `gsdml modules FILE`, `gsdml submodules FILE` | the module and submodule types, with IO lengths and records |
| `gsdml generate --model MODEL.json [-O FILE\|DIR]` | the GSDML file of a go-profinet device model |
| `connect` | establish an AR as IO-Controller and run it: states and alarms are streamed, then a summary; `--slot N` expects only some modules (shared device: another controller owns the rest), `--peer-check PORT=STATION/PORT` has the device watch a port's neighbor, `--mrp-role client\|manager\|off` gives a multiport device its MRP role, `--reparam SLOT:SUBSLOT:INDEX:HEX` re-parameterizes the running AR (PrmBegin … PrmEnd) |
| `ar` | establish an AR and show what Connect negotiated (AR, IOCRs, ModuleDiff) |
| `iocr` | the layout of the IOCRs: data objects, IOPS and IOCS at their offsets |
| `io status [--settle D]`, `io monitor [--interval D] [--changes]`, `io statistics [--duration D]` | a snapshot of the inputs and output statuses; the inputs at an interval (only changes with `--changes`); the cyclic counters over 5 s |
| `alarms` | establish an AR and stream the device's alarms |
| `disconnect --ar UUID --session-key N` | release an AR another controller left behind |
| `version`, `completion SHELL` | version; shell completion for bash, zsh, fish and PowerShell |

Every command has `--help`. A device is selected by `--station NAME` (resolved with DCP Identify, which waits `--wait`, default 1s; several answers are an error) or `--mac`; `--ip` overrides the RPC address it reports.

### Global flags

| Flag | Environment | Default | Meaning |
|------|-------------|---------|---------|
| `-i`, `--interface` | `PROFINETCTL_INTERFACE` | | the Ethernet interface |
| `-o`, `--format` | `PROFINETCTL_FORMAT` | `text` | `text`, `json` or `table` |
| `--timeout` | `PROFINETCTL_TIMEOUT` | `5s` | time limit of one request |
| `--pcap FILE` | | | record every frame sent and received (open it with Wireshark) |
| `--debug` | | | go-profinet's diagnostics on stderr, and the causes of an error |

## Connecting as IO-Controller

`connect`, `ar`, `iocr`, `io` and `alarms` act as IO-Controller (conformance class A, RT_CLASS_1, or RT_CLASS_2 with `--advanced`). They need the expected configuration of the device: its GSDML file (`--gsdml FILE`, with `--dap ID` when it has several device access points), or a go-profinet model in JSON (`--model`). The modules a GSDML file fixes or uses in slots are expected; `--plug SLOT=IDENT` plugs others from its catalog (`gsdml modules` lists them).

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
- `--slot N` (repeatable) expects only those modules (shared device), `--peer-check PORT=STATION/PORT` has the device check a port's neighbor, `--mrp-role client|manager|off` (with `--mrp-domain-name`) sets a multiport device's MRP role, and `--reparam SLOT:SUBSLOT:INDEX:HEX` re-parameterizes the running AR after `--reparam-after` (1s).

## Output

`--format text` (default) is for people, `table` aligns columns, `json` is for programs: one JSON document, or JSON Lines for the streaming commands (`connect`, `alarms`, `io monitor`, `hello listen`). JSON field names are snake_case and stable: see [docs/json-output.md](docs/json-output.md). A fatal error is one `error:` line on stderr, or with `--format json` one `{"error": {"kind", "code", "message"}}` object on stdout.

| Exit code | Meaning |
|-----------|---------|
| 0 | success |
| 2 | usage: unknown command or flag, missing selection or confirmation |
| 3 | invalid input: a flag value, GSDML or model file that does not make sense; nothing was sent |
| 4 | transport: the interface cannot be opened (privileges), a socket cannot be bound |
| 5 | timeout: the device did not answer |
| 6 | refused or protocol error: a PNIO error status, a DCP block error, a duplicate station name, a malformed answer |
| 7 | partial: a result was written and some of it failed (a GSDML file with issues, a topology check that found differences, an I&M record that could not be read) |
| 8 | not found: no device answers to the station name |
| 10 | output or internal error |
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

- IO-Controller with RT_CLASS_1 and RT_CLASS_2 (advanced startup); no IRT (RT_CLASS_3), no PROFIsafe. MRP: it gives a multiport device its role (`--mrp-role`) and reads its state (`mrp status`); it is not an MRP node itself. The automatic manager (MRA) is a role of a device's own configuration (PROFINET's MRP_Role has no value for it), so `--mrp-role` cannot set it; `mrp status` shows what such a device acts as, client or manager. Shared device: it expects a subset of the modules (`--slot`); another controller owns the rest. System redundancy (S2), an experiment of go-profinet that is not interoperable, is not exposed.
- `connect --reparam` needs a device that accepts PrmBegin in Data: go-profinet's device and profinet-py do; p-net and pnio-rs refuse it (the AR runs on, the event reports the refusal).
- OT Fabric is not a PROFIBUS & PROFINET International member: no official XSD validation, no verification of GSDX signatures (`gsdml gsdx` inspects the container), no MRCD, no certification. The full impact is in go-profinet's [docs/PI-MEMBERSHIP.md](https://github.com/otfabric/go-profinet/blob/main/docs/PI-MEMBERSHIP.md).
- `gsdml validate` checks go-profinet's catalog of the GSDML schema (structure, types, references), not the official schemas, which PROFIBUS & PROFINET International licenses to its members: it is not PI's GSDML checker.
- `disconnect` cannot release an AR at devices that tie it to the RPC activity of its Connect (p-net); they close it after the CM activity timeout.
- `topology` reads devices that have an IP address; switches without PROFINET (no DCP) appear only as neighbors.
- Timing of the cyclic exchange depends on the host: a general-purpose OS schedules cycles of 1 ms and up; see the jitter in `io statistics`.
- profinetctl is not certified by PROFIBUS & PROFINET International and claims no conformance.

## Development

```bash
make test       # unit and integration tests (race detector)
make coverage   # coverage, fails below 90 %
make check      # everything CI runs
```

The tests run profinetctl in process against go-profinet IO-Devices on a virtual Ethernet network, so they need neither privileges nor hardware. `make pnet-smoke` runs the DCP, record, I&M, diagnosis, topology and AR commands against an independent implementation, rt-labs p-net in Docker (the images of [profinet-interop](https://github.com/otfabric/profinet-interop)), on a real Linux bridge. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT, see [LICENSE](LICENSE).
