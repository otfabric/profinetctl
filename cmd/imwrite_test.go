// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"

	"github.com/otfabric/go-profinet"
	"github.com/otfabric/go-profinet/device"
	"github.com/otfabric/go-profinet/pnio"
	"github.com/otfabric/go-profinet/rpc"
	"github.com/otfabric/go-profinet/vnet"
)

// recordResponse returns the block type (IODReadResHeader or
// IODWriteResHeader) and the record index of the DCE/RPC response in an
// Ethernet frame.
func recordResponse(frame []byte) (pnio.BlockType, pnio.Index, bool) {
	const eth, udp = 14, 8
	if len(frame) < eth+20+udp || binary.BigEndian.Uint16(frame[12:]) != 0x0800 {
		return 0, 0, false
	}
	ip := frame[eth:]
	ihl := int(ip[0]&0x0F) * 4
	if ip[9] != 17 || ihl < 20 || len(ip) < ihl+udp {
		return 0, 0, false
	}
	h, body, err := rpc.DecodeHeader(ip[ihl+udp:])
	if err != nil || h.Type != rpc.TypeResponse {
		return 0, 0, false
	}
	_, args, err := pnio.DecodeNDRResponse(body, h.Order())
	if err != nil {
		return 0, 0, false
	}
	blk, _, err := pnio.ReadBlock(args)
	if err != nil {
		return 0, 0, false
	}
	if r, err := pnio.DecodeReadRes(blk); err == nil {
		return pnio.BlockReadRes, r.Index, true
	}
	if r, err := pnio.DecodeWriteRes(blk); err == nil {
		return pnio.BlockWriteRes, r.Index, true
	}
	return 0, 0, false
}

// dropResponses makes the endpoint of a device lose its responses of block
// type typ for the record index idx.
func dropResponses(ep *vnet.Endpoint, typ pnio.BlockType, idx pnio.Index) {
	ep.SetFaults(vnet.Faults{Loss: 1, Match: func(f []byte) bool {
		t, i, ok := recordResponse(f)
		return ok && t == typ && i == idx
	}})
}

// imDevice starts a device whose I&M1 is function "pump", location "hall 1",
// and returns the endpoint it answers from.
func (n *testNet) imDevice(name, ip string) *vnet.Endpoint {
	n.t.Helper()
	ep, mux, st, _ := n.endpoint(ip)
	d, err := device.New(device.Config{
		Model: testModel(), Mux: mux, StationName: name, IP: suite(ip), ListenPacket: st.ListenPacket,
		Handler: &recorder{}, NoResponseDelay: true,
		IM:     device.IMData{IM1: pnio.IM1{TagFunction: "pump", TagLocation: "hall 1"}},
		Logger: profinet.NopLogger(),
	})
	if err != nil {
		n.t.Fatal(err)
	}
	n.t.Cleanup(func() { _ = d.Close() })
	return ep
}

// im1 reads I&M1 of a station.
func (n *testNet) im1(station string) im1V {
	n.t.Helper()
	var r imResult
	n.json(&r, "-i", "pn0", "im", "read", "-s", station, "--record", "1")
	if len(r.Records) != 1 || r.Records[0].IM1 == nil {
		n.t.Fatalf("I&M1 of %s: %+v", station, r)
	}
	return *r.Records[0].IM1
}

// With only --function or only --location, im write keeps the other field of
// I&M1: when the current I&M1 cannot be read it writes nothing and exits with
// the error of the read.
func TestIMWriteNeedsTheCurrentIM1(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	ep := n.imDevice("dev-im", "192.168.0.21")
	dropResponses(ep, pnio.BlockReadRes, pnio.IndexIM1)
	for _, flag := range []string{"--function", "--location"} {
		msg := n.fails(5, "request_timeout", "-i", "pn0", "im", "write", "-s", "dev-im", flag, "valve", "--timeout", "1s")
		if !strings.HasPrefix(msg, "read I&M1: ") {
			t.Fatalf("im write %s: %s", flag, msg)
		}
	}
	ep.SetFaults(vnet.Faults{})
	if m := n.im1("dev-im"); m.TagFunction != "pump" || m.TagLocation != "hall 1" {
		t.Fatalf("I&M1 after the failed writes: %+v", m)
	}
	// Both fields given: there is nothing to keep, and the write goes ahead.
	dropResponses(ep, pnio.BlockReadRes, pnio.IndexIM1)
	n.ok("-i", "pn0", "im", "write", "-s", "dev-im", "--function", "valve", "--location", "hall 2", "--timeout", "1s")
	ep.SetFaults(vnet.Faults{})
	if m := n.im1("dev-im"); m.TagFunction != "valve" || m.TagLocation != "hall 2" {
		t.Fatalf("I&M1 after the write of both fields: %+v", m)
	}
}

// When one of several records fails after others were written, the result
// names the records written and the one that failed, stderr names those
// written, and the exit code is 7 (partial). When the first record fails,
// nothing was written: the exit code is that of its error.
func TestIMWriteNamesTheRecordsWritten(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	ep := n.imDevice("dev-im", "192.168.0.22")
	dropResponses(ep, pnio.BlockWriteRes, pnio.IndexIM3)
	args := []string{"-i", "pn0", "im", "write", "-s", "dev-im", "--function", "valve", "--date", "2026-11-15", "--descriptor", "x", "--signature", "a1", "--timeout", "1s"}
	out, errOut, code := n.run(append(args, "--format", "json")...)
	// The result as docs/json-output.md describes it.
	type imWriteJSON struct {
		Station, Operation, Value string
		Written                   []string
		Failed                    *struct{ Record, Kind, Code, Message string }
	}
	var r imWriteJSON
	if code != 7 || errOut != "warning: written before I&M3 failed: I&M1, I&M2\n" || json.Unmarshal([]byte(out), &r) != nil {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	f := r.Failed
	if strings.Join(r.Written, ", ") != "I&M1, I&M2" || r.Value != "I&M1, I&M2" || r.Station != "dev-im" || r.Operation != "im write" ||
		f == nil || f.Record != "I&M3" || f.Kind != "timeout" || f.Code != "request_timeout" || !strings.HasPrefix(f.Message, "write I&M3: ") {
		t.Fatalf("result: %s", out)
	}
	out, _, code = n.run(args...)
	if code != 7 || !strings.Contains(out, " → I&M1, I&M2\nfailed: write I&M3: ") {
		t.Fatalf("text: exit %d\n%s", code, out)
	}
	// The first record fails: none was written before it.
	dropResponses(ep, pnio.BlockWriteRes, pnio.IndexIM1)
	out, errOut, code = n.run("-i", "pn0", "im", "write", "-s", "dev-im", "--function", "valve", "--date", "2026-11-15", "--timeout", "1s", "--format", "json")
	if code != 5 || errOut != "" || !strings.Contains(out, `"request_timeout"`) {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	// Every record written: written lists them all, and nothing failed.
	ep.SetFaults(vnet.Faults{})
	r = imWriteJSON{}
	n.json(&r, "-i", "pn0", "im", "write", "-s", "dev-im", "--function", "valve", "--date", "2026-11-15", "--timeout", "1s")
	if strings.Join(r.Written, ", ") != "I&M1, I&M2" || r.Value != "I&M1, I&M2" || r.Failed != nil {
		t.Fatalf("result: %+v", r)
	}
}

// An interruption while a later record is written is not a partial result:
// the exit code stays 130, and stderr names the records written.
func TestIMWriteInterruptedAfterARecord(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	ep := n.imDevice("dev-im", "192.168.0.23")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The device's answer to the write of I&M2 is lost, and the user
	// interrupts profinetctl while it waits for it.
	ep.SetFaults(vnet.Faults{Loss: 1, Match: func(f []byte) bool {
		typ, idx, ok := recordResponse(f)
		if ok && typ == pnio.BlockWriteRes && idx == pnio.IndexIM2 {
			cancel()
			return true
		}
		return false
	}})
	var out, errOut bytes.Buffer
	code := Run(ctx, n, []string{"-i", "pn0", "im", "write", "-s", "dev-im", "--function", "valve", "--date", "2026-11-15", "--format", "json"}, &out, &errOut)
	if code != 130 || errOut.String() != "warning: written before I&M2 failed: I&M1\n" || !strings.Contains(out.String(), `"context_canceled"`) {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out.String(), errOut.String())
	}
}
