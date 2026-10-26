# Releasing profinetctl

Releases are cut by the **Release** workflow (`.github/workflows/release.yml`, the shared `otfabric/.github` `go-binary-release.yml@v2`): run it from the Actions tab with the version bump (`patch`, `minor`, `major`). It tags the version, builds the binaries for Linux (x86-64, ARM64, ARMv7), macOS (x86-64, ARM64) and Windows (x86-64, ARM64) with the version, tag, commit and build date in `profinetctl version`, and publishes them with SHA-256 checksums.

## go-profinet

profinetctl requires a released go-profinet version (go.mod). When a release needs a newer go-profinet, release go-profinet first, then `GOWORK=off go get github.com/otfabric/go-profinet@vX.Y.Z && GOWORK=off go mod tidy`.

## Checklist

- `make check` passes locally; CI is green.
- [docs/exitcodes.md](docs/exitcodes.md) and [docs/json-output.md](docs/json-output.md) describe any change of exit codes or JSON fields; a breaking change is called out in the release notes.
- `SECURITY.md` lists the supported versions.
