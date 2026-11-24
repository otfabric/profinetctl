#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Runs profinetctl against rt-labs p-net (the profinet-interop-pnet image of
# otfabric/profinet-interop, built with its "make images") on a Docker bridge
# network, command by command, and fails on the first unexpected exit code or
# JSON result. Needs Docker and python3 (for the JSON checks); builds
# profinetctl for Linux on the host's
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

# json EXPECTED-EXIT ARGS...: run with --format json; the result is in
# $WORK/out.json for check.
json() {
	want=$1
	shift
	echo "### profinetctl $* --format json"
	set +e
	docker run --rm --network "$NET" --ip "$SUBNET.3" --mac-address 02:42:0a:8d:07:03 --cap-add NET_RAW --cap-add NET_ADMIN \
		-v "$WORK":/w "$TOOL_IMAGE" /w/profinetctl -i eth0 "$@" --format json >"$WORK/out.json"
	got=$?
	set -e
	if [ "$got" != "$want" ]; then
		cat "$WORK/out.json" >&2
		echo "FAIL: exit $got, want $want" >&2
		exit 1
	fi
}

# check EXPR...: each Python expression is true of the last json result,
# parsed as r (python3 on the host); e(r) is the error object, m(r) its
# message.
check() {
	python3 -I - "$WORK/out.json" "$@" <<'PY'
import json, sys
r = json.load(open(sys.argv[1]))
def e(r): return r["error"]
def m(r): return r["error"]["message"]
for x in sys.argv[2:]:
    if not eval(x, {"r": r, "e": e, "m": m, "any": any, "all": all, "len": len}):
        sys.exit("FAIL: not true of the result: " + x + "\n" + json.dumps(r, indent=1))
PY
}

# waitlog CONTAINER TEXT SECONDS: wait until the log of CONTAINER contains
# TEXT; fail after SECONDS or when the container ends first.
waitlog() {
	i=0
	until docker logs "$1" 2>&1 | grep -qF -- "$2"; do
		if [ "$(docker inspect -f '{{.State.Running}}' "$1")" != true ] || [ "$i" -ge "$3" ]; then
			docker logs "$1" 2>&1 | tail -20 >&2
			echo "FAIL: $1 did not log $2 within $3 s" >&2
			exit 1
		fi
		i=$((i + 1))
		sleep 1
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
# second profinetctl holds one, until it is stopped, for the commands below.
# Where p-net does not have the API it answers a Read with an empty NDR
# response (pf_cmrpc.c), a protocol error for profinetctl; for one it has it
# answers I&M and the diagnosis of slot 1 whatever the API: its sample keys
# them by slot and subslot. So 0x4E01 answered and 0x4E02 not is what shows
# that --api reaches the device. After the ARs above with slot 1 in API 0,
# p-net's RealIdentificationData lists slot 1 in API 0 as well as in API
# 0x4E01 (observed; a fresh p-net lists 0x4E01 only), so device info is
# checked for the 0x4E01 entry, not for the whole list.
#
# Not run: record read of a user record (index 0x7FFF or below). For a Read
# Implicit p-net has no AR (pf_cmrpc.c, "Assume the implicit AR") and
# pf_fspm_cm_read_ind dereferences it for the user record callback: p-net
# ea5e554, the tree profinet-interop builds, ends with a segmentation fault,
# in any API, with or without an AR. Fixed upstream after that tree:
# rtlabs-com/p-net issue #594, commit c752521.
docker run -d --name "$NET-ar" --network "$NET" --ip "$SUBNET.4" --mac-address 02:42:0a:8d:07:04 --cap-add NET_RAW --cap-add NET_ADMIN \
	-v "$WORK":/w "$TOOL_IMAGE" /w/profinetctl -i eth0 connect -s pnet-dut --model /w/pnet-model-api.json \
	--reduction-ratio 8 --data-hold-factor 10 --watchdog-factor 10 --duration 0 --format json >/dev/null
waitlog "$NET-ar" '"to":"Running"' 20
json 0 device info -s pnet-dut
check 'r["api_discovery"] == "apidata"' \
	'any(x["api"] == 0x4E01 and x["slot"] == 1 and x["module_ident_number"] == 0x32 and [s["subslot"] for s in x["submodules"]] == [1] for x in r["modules"])' \
	'[(x["api"], x["slot"]) for x in r["modules"]][0] == (0, 0)'
json 0 im read -s pnet-dut --api 0x4E01 --slot 1 --subslot 1
check '(r["api"], r["slot"], r["subslot"]) == (0x4E01, 1, 1)' 'r["records"][0]["record"] == "I&M0"' 'r["records"][0]["im0"]["vendor_id"] == 1171' \
	'all("error" not in x for x in r["records"])'
json 6 im read -s pnet-dut --api 0x4E02 --slot 1 --subslot 1
check '(e(r)["kind"], e(r)["code"]) == ("protocol", "protocol_violation")' 'm(r).startswith("I&M0: ")' '"read 19970/1/0x0001 index 0xAFF0" in m(r)'
json 0 diagnostics -s pnet-dut --api 0x4E01 --slot 1 --subslot 1
check 'r["entries"] == []' 'r["station"] == "pnet-dut"'
json 6 diagnostics -s pnet-dut --api 0x4E02 --slot 1 --subslot 1
check '(e(r)["kind"], e(r)["code"]) == ("protocol", "protocol_violation")' '"read 19970/1/0x0001 index 0x800C" in m(r)'
json 2 diagnostics -s pnet-dut --api 0x4E01
check 'e(r)["code"] == "invalid_flag_combination"'
# The AR ran through every check: still Running, never left it, and it is
# released at SIGTERM (exit code 0).
[ "$(docker inspect -f '{{.State.Running}}' "$NET-ar")" = true ] || { docker logs "$NET-ar" >&2; echo "FAIL: the AR ended during the checks" >&2; exit 1; }
if docker logs "$NET-ar" 2>&1 | grep -qF '"from":"Running"'; then
	docker logs "$NET-ar" >&2
	echo "FAIL: the AR left Running during the checks" >&2
	exit 1
fi
docker stop -t 10 "$NET-ar" >/dev/null
[ "$(docker inspect -f '{{.State.ExitCode}}' "$NET-ar")" = 0 ] || { docker logs "$NET-ar" >&2; echo "FAIL: the AR did not end cleanly" >&2; exit 1; }
docker rm -f "$NET-ar" >/dev/null

# API 0x4E01 at go-profinet's device (not an independent implementation:
# the device profinetctl is built on). It checks the API of every Read:
# invalid area/API (0xB4) for a submodule of another API, invalid index
# (0xB0) for I&M outside the DAP (go-profinet device/records.go).
docker run -d --name "$NET-otf" --network "$NET" --ip "$SUBNET.20" --cap-add NET_RAW --cap-add NET_ADMIN \
	-v "$WORK":/w "$TOOL_IMAGE" /w/otfnode device -i eth0 -station otf-api -model /w/otf-device-api.json >/dev/null
waitlog "$NET-otf" '"event":"start"' 20
json 0 device info -s otf-api
check 'r["api_discovery"] == "apidata"' \
	'[(x["api"], x["slot"], x["module_ident_number"]) for x in r["modules"]] == [(0, 0, 0x100), (0, 1, 0x10), (0, 2, 0x20), (0x4E01, 3, 0x30)]' \
	'r["modules"][3]["submodules"] == [{"subslot": 1, "submodule_ident_number": 0x31}]'
json 0 record read -s otf-api --api 0x4E01 --slot 3 --subslot 1 --index 0x11
check '(r["api"], r["slot"], r["subslot"], r["index"], r["length"], r["data"]) == (0x4E01, 3, 1, 0x11, 2, "0002")'
json 0 record read -s otf-api --api 0x4E01 --slot 3 --subslot 1 --index 0xB900
check '(r["api"], r["slot"], r["subslot"], r["index"], r["length"], r["data"]) == (0x4E01, 3, 1, 0xB900, 4, "80010011")'
json 6 record read -s otf-api --slot 3 --subslot 1 --index 0x11
check 'e(r)["code"] == "pnio_error"' '"read 0/3/0x0001 index 0x0011" in m(r)' '"DE 80 B4 00" in m(r)'
json 6 record read -s otf-api --api 0x4E02 --slot 3 --subslot 1 --index 0x11
check 'e(r)["code"] == "pnio_error"' '"read 19970/3/0x0001 index 0x0011" in m(r)' '"DE 80 B4 00" in m(r)'
json 0 im read -s otf-api
check '(r["api"], r["slot"], r["subslot"]) == (0, 0, 1)' 'r["records"][0]["im0"]["order_id"] == "OTF-INTEROP-1"'
json 6 im read -s otf-api --api 0x4E01 --slot 3 --subslot 1
check 'e(r)["code"] == "pnio_error"' 'm(r).startswith("I&M0: ")' '"read 19969/3/0x0001 index 0xAFF0" in m(r)' '"DE 80 B0 00" in m(r)'
json 6 im read -s otf-api --slot 3 --subslot 1
check 'e(r)["code"] == "pnio_error"' '"read 0/3/0x0001 index 0xAFF0" in m(r)' '"DE 80 B4 00" in m(r)'
json 0 diagnostics -s otf-api --api 0x4E01 --slot 3 --subslot 1
check 'r["entries"] == []' 'r["station"] == "otf-api"'
json 6 diagnostics -s otf-api --slot 3 --subslot 1
check 'e(r)["code"] == "pnio_error"' '"read 0/3/0x0001 index 0x800C" in m(r)' '"DE 80 B4 00" in m(r)'
docker rm -f "$NET-otf" >/dev/null
run 0 name set pnet-renamed -s pnet-dut
run 0 name set pnet-dut -s pnet-renamed
run 0 ip set "$SUBNET.11/24" -s pnet-dut
run 0 ip get -s pnet-dut
run 2 reset -s pnet-dut
run 0 reset -s pnet-dut --mode communication --confirm pnet-dut
run 11 identify -s pnet-dut --wait 500ms
echo "profinetctl smoke test against p-net passed"
