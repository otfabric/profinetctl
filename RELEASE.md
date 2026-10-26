# Releasing profinetctl

Releases are cut by the **Release** workflow (`.github/workflows/release.yml`, the shared `otfabric/.github` `go-binary-release.yml@v2`): run it from the Actions tab with the version bump (`patch`, `minor`, `major`). It tags the version, builds the binaries for Linux (x86-64, ARM64, ARMv7), macOS (x86-64, ARM64) and Windows (x86-64, ARM64) with the version, tag, commit and build date in `profinetctl version`, and publishes them with SHA-256 checksums.

## Before the first release

profinetctl builds against a sibling checkout of go-profinet (`replace` in go.mod). Before releasing:

1. Release go-profinet (its first tag).
2. In go.mod, require that tag and delete the `replace` directive; `GOWORK=off go mod tidy`.
3. Replace the side-by-side checkout job of `.github/workflows/ci.yml` with the shared `otfabric/.github/.github/workflows/go-ci.yml@v2` workflow, as in dnp3ctl.

## Checklist

- `make check` passes locally; CI is green.
- [docs/exitcodes.md](docs/exitcodes.md) and [docs/json-output.md](docs/json-output.md) describe any change of exit codes or JSON fields; a breaking change is called out in the release notes.
- `SECURITY.md` lists the supported versions.
