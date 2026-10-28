# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in `profinetctl`, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email **security@otfabric.io**, or open a private advisory on GitHub (the [Security](https://github.com/otfabric/profinetctl/security) tab → **Advisories** → **Report a vulnerability**), with a description, steps to reproduce, the affected version (`profinetctl version`) and the impact you identified.

We will acknowledge your report within 48 hours and aim to provide a fix or mitigation within 7 days for critical issues.

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.3.x | Yes |
| < 0.3 | No |

## Security Considerations

### PROFINET has no built-in security

PROFINET IO (without the Security Class 1–3 extensions, which profinetctl does not implement) has no authentication and no encryption. Anyone on the Layer 2 network of a device can rename it, change its IP address, reset it, write its records and connect to it as IO-Controller. profinetctl does not change that.

- **Use it only on devices and networks you own or are authorized to work on.**
- **Keep PROFINET networks isolated** from office networks and the Internet; DCP and the cyclic data are Layer 2 and must not cross into untrusted segments.

### Commands that change a device

| Command | Effect |
|---------|--------|
| `name set`, `ip set` | change the station name or IP address; a running AR with an IO-Controller that finds the device by its name or address breaks, and the controller cannot find the device again until the name is restored |
| `reset` | DCP ResetToFactory: erases configuration (by `--mode`) and aborts the device's ARs. It needs `--confirm` with the target's station name or MAC address |
| `record write`, `im write` | write records in a device access AR; a parameter record changes how the device behaves |
| `connect --param`, `--reparam` | write parameter records before PrmEnd, or re-parameterize the running AR: the device's behaviour changes |
| `connect --mrp-role` | writes the MRP records of a multiport device: changes its ring redundancy role; a wrong role can open or loop a ring |
| `connect --peer-check` | writes PDPortDataCheck: the device reports a diagnosis when the port's neighbor differs |
| `connect --assign-ip` (`--permanent-ip`) | sets the IP address with DCP before connecting, permanently with `--permanent-ip` |
| `connect`, `ar`, `iocr`, `io`, `alarms` | establish an AR as IO-Controller. A device accepts one controller AR (or a few): connecting to a device that a PLC controls is refused, or takes the device from it when the PLC's AR is down. With `--output` profinetctl drives the device's outputs: **actuators move**. Outputs not given are sent with IOPS *bad*, which keeps them in their safe state |
| `disconnect` | releases an AR: the device stops its outputs (safe state) |
| `signal` | flashes the LED; harmless |

Read-only commands (`discover`, `identify`, `name get`, `ip get`, `record read`, `im read`, `diagnostics`, `device info`, `status`, `lldp neighbors`, `topology`, `topology check`, `mrp status`) send DCP requests and ReadImplicit calls only; `hello listen` only listens, and the `gsdml` commands read local files. DCP Identify is multicast: every device on the segment answers.

### Privileges

profinetctl needs raw Ethernet access (root, CAP_NET_RAW, `/dev/bpf*` or Npcap). Prefer granting the capability to the binary (`setcap cap_net_raw,cap_net_admin+ep`) over running it as root, and do not grant it on shared hosts.

### Input files

GSDML files are parsed with a size limit (64 MiB) and without external entities or DTD processing; model files are JSON. `--pcap` writes every frame, including record data, to the file you name.

### Not certified

profinetctl is not certified by PROFIBUS & PROFINET International and claims no conformance to the PROFINET specification. Do not use it as a safety function.
