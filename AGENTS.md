# AGENTS.md — profinetctl

## What this is

Command-line tool for PROFINET IO built on [go-profinet](https://github.com/otfabric/go-profinet): discovery, names and addresses, records, I&M, diagnosis, GSDML, topology and cyclic IO. Pure Go, one binary. Profile: **cli**.

## Organisation standards

This repository follows **otfabric/engineering policy 1.0**, profile **cli** (`.otfabric/policy.yaml`, mode `baseline`). Where this file and the organisation standards disagree, this file wins here — say so in your report.

## Where the truth lives

| Question | Source |
|---|---|
| Exit codes (script-facing API) | docs/exitcodes.md |
| JSON output contract | docs/json-output.md |
| Commands and usage | README.md |
| How to contribute | CONTRIBUTING.md |
| Protocol behaviour | go-profinet |
| Release history | RELEASE.md (`## vX.Y.Z` … `---`) |

## Hard rules

1. Exit codes and JSON output are a public contract: a change is called out in RELEASE.md and documented in docs/exitcodes.md or docs/json-output.md.
2. Never lower `COVERAGE_MIN` or the CI `coverage-min`, add lint suppressions without a reason, or skip or delete tests to make CI pass.
3. Never invent protocol semantics or claim interoperability or conformance beyond recorded evidence; protocol questions go to go-profinet.
4. `github.com/otfabric/*` dependencies are pinned to released tags; no committed `replace` directives.
5. Protocol behaviour belongs in go-profinet; presentation, flags and exit codes here.
6. Commands that change a device validate every flag before sending anything; `reset` requires `--confirm`.
7. Errors returned by commands are typed (`internal/errs`); go-profinet errors go through `classify`.

## Commands

| When you change… | Run |
|---|---|
| anything | `make check` |
| 32-bit relevant code (linux/arm/v7 is a release target) | `make cross` |
| RELEASE.md | `make release-notes-check` |
| documented examples | `make examples-doc` (Docker, p-net) |
| behaviour against p-net | `make pnet-smoke` (Docker; needs the images of the sibling repository profinet-interop) |

## Definition of Done

`make check` green; tests for new commands, error paths included; exit codes and JSON documented; RELEASE.md section for user-visible changes.

## Git and releases

Branches and PRs. Do not push to `main`, tag or run the Release workflow unless the maintainer asks in the session.
