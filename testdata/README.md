# Test data

Models for `scripts/pnet-smoke.sh`, copied from go-profinet's
`interop/testdata` (at 909ec56):

- `pnet-model.json`: the expected configuration of the rt-labs p-net sample
  application, as a go-profinet model. The identity and module numbers are
  those of the p-net sample application (GPLv3), described here as facts.
- `pnet-model-api.json`: the same with the IO module's submodule (slot 1,
  subslot 1) in API 0x4E01. p-net, built with two APIs by profinet-interop
  (v0.12.0 and later), creates that API at Connect.
- `otf-device-api.json`: the device model of go-profinet's interop device
  (otfnode) with the IO module of slot 3 in API 0x4E01 and records 0x0011,
  0xB600 and the read-only 0xB900 on it.
