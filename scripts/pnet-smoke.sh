#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Runs profinetctl against rt-labs p-net (the profinet-interop-pnet image of
# otfabric/profinet-interop, built with its "make images") on a Docker bridge
# network, command by command, and fails on the first unexpected exit code.
# Needs Docker; builds profinetctl for Linux on the host's architecture.
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d)
NET=profinetctl-smoke
SUBNET=${SMOKE_SUBNET:-10.141.7}
PNET_IMAGE=${PROFINET_INTEROP_PNET_IMAGE:-profinet-interop-pnet:dev}
TOOL_IMAGE=${SMOKE_TOOL_IMAGE:-debian:bookworm-slim}
ARCH=$(docker version --format '{{.Server.Arch}}')

cleanup() {
	docker rm -f "$NET-pnet" >/dev/null 2>&1 || true
	docker network rm "$NET" >/dev/null 2>&1 || true
	rm -rf "$WORK"
}
trap cleanup EXIT

(cd "$ROOT" && GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -o "$WORK/profinetctl" .)
cp "$ROOT/testdata/pnet-model.json" "$WORK/"
docker network create --subnet "$SUBNET.0/24" "$NET" >/dev/null
docker run -d --name "$NET-pnet" --network "$NET" --ip "$SUBNET.10" --cap-add NET_RAW --cap-add NET_ADMIN "$PNET_IMAGE" device --station pnet-dut >/dev/null
sleep 6

# run EXPECTED-EXIT ARGS...
run() {
	want=$1
	shift
	echo "### profinetctl $*"
	set +e
	docker run --rm --network "$NET" --ip "$SUBNET.3" --mac-address 02:42:0a:8d:07:03 --cap-add NET_RAW --cap-add NET_ADMIN \
		-v "$WORK":/w "$TOOL_IMAGE" /w/profinetctl -i eth0 "$@"
	got=$?
	set -e
	if [ "$got" != "$want" ]; then
		echo "FAIL: exit $got, want $want" >&2
		docker logs "$NET-pnet" 2>&1 | tail -20 >&2
		exit 1
	fi
}

M="--model /w/pnet-model.json"
run 0 discover --wait 2s
run 0 identify -s pnet-dut
run 0 im read -s pnet-dut
run 0 device info -s pnet-dut
run 0 diagnostics -s pnet-dut
run 0 status -s pnet-dut
run 0 record read -s pnet-dut --index 0xF841
run 0 topology --wait 2s
run 0 signal -s pnet-dut
run 6 record write -s pnet-dut --slot 1 --index 123 --data 01020304
run 0 record write -s pnet-dut --slot 1 --index 123 --data 01020304 $M
run 0 im write -s pnet-dut --location "bench 2" $M
run 0 ar -s pnet-dut $M --reduction-ratio 8 --data-hold-factor 10 --watchdog-factor 10
run 0 iocr -s pnet-dut $M
run 0 io status -s pnet-dut $M --output 2:1:1122334455667788 --reduction-ratio 8 --data-hold-factor 10 --watchdog-factor 10 --settle 1s
run 0 io statistics -s pnet-dut $M --reduction-ratio 8 --data-hold-factor 10 --watchdog-factor 10 --duration 2s
run 0 connect -s pnet-dut $M --reduction-ratio 8 --data-hold-factor 10 --watchdog-factor 10 --duration 2s --format json
run 0 name set pnet-renamed -s pnet-dut
run 0 name set pnet-dut -s pnet-renamed
run 0 ip set "$SUBNET.11/24" -s pnet-dut
run 0 ip get -s pnet-dut
run 2 reset -s pnet-dut
run 0 reset -s pnet-dut --mode communication --confirm pnet-dut
run 8 identify -s pnet-dut --wait 500ms
echo "profinetctl smoke test against p-net passed"
