#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Runs profinetctl against rt-labs p-net (the profinet-interop-pnet image of
# otfabric/profinet-interop, built with its "make images") on a Docker bridge
# network, command by command, and fails on the first unexpected exit code or
# JSON result. Needs Docker; builds profinetctl for Linux on the host's
# architecture, and go-profinet's interop device (otfnode, of the go-profinet
# version in go.mod) for the commands that address an API other than 0.
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d)
NET=profinetctl-smoke
SUBNET=${SMOKE_SUBNET:-10.141.7}
PNET_IMAGE=${PROFINET_INTEROP_PNET_IMAGE:-profinet-interop-pnet:dev}
TOOL_IMAGE=${SMOKE_TOOL_IMAGE:-debian:bookworm-slim}
ARCH=$(docker version --format '{{.Server.Arch}}')

cleanup() {
	docker rm -f "$NET-pnet" "$NET-otf" "$NET-ar" >/dev/null 2>&1 || true
	docker network rm "$NET" >/dev/null 2>&1 || true
	rm -rf "$WORK"
}
trap cleanup EXIT

(cd "$ROOT" && GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -o "$WORK/profinetctl" .)
(cd "$ROOT" && GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -tags interop -o "$WORK/otfnode" github.com/otfabric/go-profinet/interop/otfnode)
cp "$ROOT/testdata/pnet-model.json" "$ROOT/testdata/pnet-model-api.json" "$ROOT/testdata/otf-device-api.json" "$WORK/"
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

# json EXPECTED-EXIT ARGS...: run with --format json; the result, without
# white space, is in $WORK/out for has and hasnt.
json() {
	want=$1
	shift
	echo "### profinetctl $* --format json"
	set +e
	docker run --rm --network "$NET" --ip "$SUBNET.3" --mac-address 02:42:0a:8d:07:03 --cap-add NET_RAW --cap-add NET_ADMIN \
		-v "$WORK":/w "$TOOL_IMAGE" /w/profinetctl -i eth0 "$@" --format json >"$WORK/out.raw"
	got=$?
	set -e
	tr -d ' \n\t' <"$WORK/out.raw" >"$WORK/out"
	if [ "$got" != "$want" ]; then
		cat "$WORK/out.raw" >&2
		echo "FAIL: exit $got, want $want" >&2
		exit 1
	fi
}

# has TEXT...: the last json result contains each TEXT (compared without
# white space).
has() {
	for want in "$@"; do
		if ! grep -qF -- "$want" "$WORK/out"; then
			cat "$WORK/out.raw" >&2
			echo "FAIL: result lacks $want" >&2
			exit 1
		fi
	done
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

# API 0x4E01 at p-net. p-net creates an API at Connect (pf_cmdev.c), so its
# slot 1 is in API 0x4E01 only while an AR with pnet-model-api.json runs: a
# second profinetctl holds one for the commands below. Where p-net does not
# have the API it answers a Read with an empty NDR response (pf_cmrpc.c), a
# protocol error for profinetctl; for one it has it answers I&M and the
# diagnosis of slot 1 whatever the API: its sample keys them by slot and
# subslot. So 0x4E01 answered and 0x4E02 not is what shows that --api
# reaches the device. Not run: record read of a user record (index <=
# 0x7FFF). For a Read Implicit p-net has no AR (pf_cmrpc.c, "Assume the
# implicit AR") and pf_fspm_cm_read_ind dereferences it for the user
# record callback: p-net (ea5e554) ends with a segmentation fault, in any
# API, with or without an AR.
docker run -d --name "$NET-ar" --network "$NET" --ip "$SUBNET.4" --mac-address 02:42:0a:8d:07:04 --cap-add NET_RAW --cap-add NET_ADMIN \
	-v "$WORK":/w "$TOOL_IMAGE" /w/profinetctl -i eth0 connect -s pnet-dut --model /w/pnet-model-api.json \
	--reduction-ratio 8 --data-hold-factor 10 --watchdog-factor 10 --duration 40s --format json >/dev/null
sleep 5
json 0 device info -s pnet-dut
has '"api":19969,"slot":1,"module_ident_number":50,' '"api":0,"slot":2,' '"api_discovery":"apidata"'
json 0 im read -s pnet-dut --api 0x4E01 --slot 1 --subslot 1
has '"api":19969,"slot":1,"subslot":1,' '"record":"I\u0026M0","im0":{"vendor_id":1171,'
json 6 im read -s pnet-dut --api 0x4E02 --slot 1 --subslot 1
has '"kind":"protocol","code":"protocol_violation"' 'read19970/1/0x0001index0xAFF0'
json 0 diagnostics -s pnet-dut --api 0x4E01 --slot 1 --subslot 1
has '"entries":[]'
json 6 diagnostics -s pnet-dut --api 0x4E02 --slot 1 --subslot 1
has '"kind":"protocol"' 'read19970/1/0x0001index0x800C'
json 2 diagnostics -s pnet-dut --api 0x4E01
has '"code":"invalid_flag_combination"'
docker rm -f "$NET-ar" >/dev/null
sleep 2

# API 0x4E01 at go-profinet's device (not an independent implementation:
# the device profinetctl is built on). It checks the API of every Read:
# invalid area/API (0xB4) for a submodule of another API, invalid index
# (0xB0) for I&M outside the DAP (go-profinet device/records.go).
docker run -d --name "$NET-otf" --network "$NET" --ip "$SUBNET.20" --cap-add NET_RAW --cap-add NET_ADMIN \
	-v "$WORK":/w "$TOOL_IMAGE" /w/otfnode device -i eth0 -station otf-api -model /w/otf-device-api.json >/dev/null
sleep 3
json 0 device info -s otf-api
has '"api":19969,"slot":3,"module_ident_number":48,"submodules":[{"subslot":1,"submodule_ident_number":49}]' '"api":0,"slot":1,' '"api_discovery":"apidata"'
json 0 record read -s otf-api --api 0x4E01 --slot 3 --subslot 1 --index 0x11
has '"api":19969,"slot":3,"subslot":1,"index":17,"length":2,"data":"0002"'
json 0 record read -s otf-api --api 0x4E01 --slot 3 --subslot 1 --index 0xB900
has '"index":47360,"length":4,"data":"80010011"'
json 6 record read -s otf-api --slot 3 --subslot 1 --index 0x11
has '"code":"pnio_error"' 'DE80B400'
json 6 record read -s otf-api --api 0x4E02 --slot 3 --subslot 1 --index 0x11
has '"code":"pnio_error"' 'read19970/3/0x0001' 'DE80B400'
json 0 im read -s otf-api
has '"api":0,"slot":0,"subslot":1,' '"order_id":"OTF-INTEROP-1"'
json 6 im read -s otf-api --api 0x4E01 --slot 3 --subslot 1
has '"code":"pnio_error"' 'I\u0026M0:' 'DE80B000'
json 6 im read -s otf-api --slot 3 --subslot 1
has '"code":"pnio_error"' 'DE80B400'
json 0 diagnostics -s otf-api --api 0x4E01 --slot 3 --subslot 1
has '"entries":[]'
json 6 diagnostics -s otf-api --slot 3 --subslot 1
has '"code":"pnio_error"' 'index0x800C' 'DE80B400'
docker rm -f "$NET-otf" >/dev/null
run 0 name set pnet-renamed -s pnet-dut
run 0 name set pnet-dut -s pnet-renamed
run 0 ip set "$SUBNET.11/24" -s pnet-dut
run 0 ip get -s pnet-dut
run 2 reset -s pnet-dut
run 0 reset -s pnet-dut --mode communication --confirm pnet-dut
run 11 identify -s pnet-dut --wait 500ms
echo "profinetctl smoke test against p-net passed"
