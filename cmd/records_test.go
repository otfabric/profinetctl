// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-profinet"
	"github.com/otfabric/go-profinet/device"
	"github.com/otfabric/go-profinet/pnio"
)

func TestRecordReadWrite(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	var r recordResult
	n.json(&r, "-i", "pn0", "record", "read", "-s", "dev-a", "--index", "0xAFF0")
	if r.Index != 0xAFF0 || r.Length != 60 || !strings.HasPrefix(r.Data, "00200038") || len(r.Blocks) != 1 || r.Blocks[0] != "I&M0" {
		t.Fatalf("I&M0 %+v", r)
	}
	out := n.ok("-i", "pn0", "record", "read", "-s", "dev-a", "--slot", "2", "--subslot", "1", "--index", "16")
	if !strings.Contains(out, "index 0x0010: 2 octets") || !strings.Contains(out, "0000  00 01") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("-i", "pn0", "record", "read", "-s", "dev-a", "--slot", "2", "--index", "0x10", "-o", "table"); !strings.Contains(out, "0001") {
		t.Fatalf("table:\n%s", out)
	}
	// A write in a device access AR, read back.
	var ch changeResult
	n.json(&ch, "-i", "pn0", "record", "write", "-s", "dev-a", "--slot", "2", "--index", "0x10", "--data", "00 07")
	if ch.Operation != "record write" || !strings.Contains(ch.Value, "2 octets") {
		t.Fatalf("write %+v", ch)
	}
	n.json(&r, "-i", "pn0", "record", "read", "-s", "dev-a", "--slot", "2", "--index", "0x10")
	if r.Data != "0007" {
		t.Fatalf("read back %+v", r)
	}
	file := writeFile(t, "rec.bin", []byte{0, 9})
	n.ok("-i", "pn0", "record", "write", "-s", "dev-a", "--slot", "2", "--index", "0x10", "--data-file", file)
	n.json(&r, "-i", "pn0", "record", "read", "-s", "dev-a", "--slot", "2", "--index", "0x10")
	if r.Data != "0009" {
		t.Fatalf("read back from file %+v", r)
	}
	// The device refuses: a PNIO error status, exit code 6.
	msg := n.fails(6, "pnio_error", "-i", "pn0", "record", "write", "-s", "dev-a", "--slot", "2", "--index", "0x10", "--data", "01")
	if !strings.Contains(msg, "write 0/2/0x0001 index 0x0010") {
		t.Fatalf("message %q", msg)
	}
	n.fails(6, "pnio_error", "-i", "pn0", "record", "read", "-s", "dev-a", "--slot", "9", "--index", "0x10")
	// Usage.
	n.fails(2, "invalid_input", "-i", "pn0", "record", "read", "-s", "dev-a")
	n.fails(3, "invalid_input", "-i", "pn0", "record", "read", "-s", "dev-a", "--index", "zz")
	n.fails(2, "invalid_input", "-i", "pn0", "record", "write", "-s", "dev-a", "--index", "1")
	n.fails(2, "invalid_flag_combination", "-i", "pn0", "record", "write", "-s", "dev-a", "--index", "1", "--data", "00", "--data-file", file)
	n.fails(3, "invalid_input", "-i", "pn0", "record", "write", "-s", "dev-a", "--index", "1", "--data", "0g")
	n.fails(3, "input_file_open_failed", "-i", "pn0", "record", "write", "-s", "dev-a", "--index", "1", "--data-file", file+".none")
	n.fails(3, "invalid_input", "-i", "pn0", "record", "read", "-s", "dev-a", "--index", "1", "--ip", "nope")
}

func TestRecordWithoutIPOrDeviceAccess(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("no-ip", "")
	n.device("no-sar", "192.168.0.11", func(c *device.Config) { c.RejectSupervisorAR = true })
	msg := n.fails(6, "invalid_input", "-i", "pn0", "record", "read", "-s", "no-ip", "--index", "0xAFF0")
	if !strings.Contains(msg, "has no IP address") {
		t.Fatalf("message %q", msg)
	}
	// --ip overrides the address the device reports.
	n.fails(5, "request_timeout", "-i", "pn0", "record", "read", "-s", "no-ip", "--index", "0xAFF0", "--ip", "192.168.0.99", "--timeout", "300ms")
	msg = n.fails(6, "pnio_error", "-i", "pn0", "record", "write", "-s", "no-sar", "--slot", "2", "--index", "0x10", "--data", "0001")
	if !strings.Contains(msg, "give --gsdml or --model") {
		t.Fatalf("message %q", msg)
	}
	n.fails(6, "pnio_error", "-i", "pn0", "im", "write", "-s", "no-sar", "--descriptor", "x")
	// With a device description the writes go through an IOCAR.
	gf := gsdmlFile(t)
	n.ok("-i", "pn0", "record", "write", "-s", "no-sar", "--slot", "2", "--index", "0x10", "--data", "0005", "--gsdml", gf)
	var r recordResult
	n.json(&r, "-i", "pn0", "record", "read", "-s", "no-sar", "--slot", "2", "--index", "0x10")
	if r.Data != "0005" {
		t.Fatalf("written in an IOCAR: %+v", r)
	}
	n.ok("-i", "pn0", "im", "write", "-s", "no-sar", "--location", "hall 9", "--model", modelFile(t))
	var im imResult
	n.json(&im, "-i", "pn0", "im", "read", "-s", "no-sar", "--record", "1")
	if im.Records[0].IM1 == nil || im.Records[0].IM1.TagLocation != "hall 9" || im.Records[0].IM1.TagFunction != "pump" {
		t.Fatalf("I&M1 written in an IOCAR: %+v", im)
	}
	n.fails(2, "invalid_flag_combination", "-i", "pn0", "record", "write", "--mac", "02:00:00:00:00:11", "--index", "1", "--data", "00", "--gsdml", gf)
	n.fails(2, "invalid_flag_combination", "-i", "pn0", "im", "write", "--mac", "02:00:00:00:00:11", "--descriptor", "x", "--gsdml", gf)
	n.fails(3, "invalid_gsdml", "-i", "pn0", "record", "write", "-s", "no-sar", "--index", "1", "--data", "00", "--gsdml", gf, "--dap", "nope")
	n.fails(11, "", "-i", "pn0", "record", "write", "-s", "nobody", "--index", "1", "--data", "00", "--gsdml", gf, "--wait", "200ms")
}

func TestIM(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	var r imResult
	n.json(&r, "-i", "pn0", "im", "read", "-s", "dev-a")
	if len(r.Records) != 5 || r.Records[0].IM0 == nil || r.Records[0].IM0.OrderID != "OTF-CTL-TEST" || r.Records[0].IM0.SoftwareRevision != "V1.2.0" ||
		len(r.Records[0].IM0.Supported) != 4 || r.Records[1].IM1 == nil || r.Records[1].IM1.TagFunction != "pump" || r.Records[4].IM4 == nil {
		t.Fatalf("I&M %+v", r)
	}
	out := n.ok("-i", "pn0", "im", "read", "-s", "dev-a")
	for _, want := range []string{`I&M0: vendor 0xF000, order "OTF-CTL-TEST", serial "0042", hardware 3, software V1.2.0`, `I&M1: function "pump", location "hall 1"`, "I&M2:", "I&M3:", "I&M4: signature 0000"} {
		if !strings.Contains(out, want) {
			t.Fatalf("text lacks %q:\n%s", want, out)
		}
	}
	if out := n.ok("-i", "pn0", "im", "read", "-s", "dev-a", "-o", "table"); !strings.Contains(out, "RECORD") || !strings.Contains(out, "hall 1") {
		t.Fatalf("table:\n%s", out)
	}
	// Write I&M1 (one of its fields), I&M2, I&M3 and I&M4.
	var ch changeResult
	n.json(&ch, "-i", "pn0", "im", "write", "-s", "dev-a", "--location", "hall 2", "--date", "2026-10-26 12:00", "--descriptor", "commissioned", "--signature", "a1b2")
	if ch.Value != "I&M1, I&M2, I&M3, I&M4" {
		t.Fatalf("write %+v", ch)
	}
	n.json(&r, "-i", "pn0", "im", "read", "-s", "dev-a", "--record", "1,2,3,4")
	if len(r.Records) != 4 || r.Records[0].IM1.TagFunction != "pump" || r.Records[0].IM1.TagLocation != "hall 2" || r.Records[1].IM2.Date != "2026-10-26 12:00" ||
		r.Records[2].IM3.Descriptor != "commissioned" || !strings.HasPrefix(r.Records[3].IM4.Signature, "a1b2") {
		t.Fatalf("after write %+v", r)
	}
	// I&M of a submodule that has none: errors per record, exit code 7.
	out, _, code := n.run("-i", "pn0", "im", "read", "-s", "dev-a", "--slot", "1", "--record", "0,1", "--format", "json")
	if code != 7 || !strings.Contains(out, `"error"`) {
		t.Fatalf("partial %d:\n%s", code, out)
	}
	if out, _, code := n.run("-i", "pn0", "im", "read", "-s", "dev-a", "--slot", "1"); code != 6 || out != "" {
		t.Fatalf("no I&M0: %d %q", code, out)
	}
	n.fails(3, "invalid_input", "-i", "pn0", "im", "read", "-s", "dev-a", "--record", "5")
	n.fails(2, "invalid_input", "-i", "pn0", "im", "write", "-s", "dev-a")
	n.fails(3, "invalid_input", "-i", "pn0", "im", "write", "-s", "dev-a", "--date", "yesterday")
	n.fails(3, "invalid_input", "-i", "pn0", "im", "write", "-s", "dev-a", "--signature", strings.Repeat("00", 60))
	n.fails(3, "invalid_input", "-i", "pn0", "im", "write", "-s", "dev-a", "--function", strings.Repeat("x", 40))
}

func TestDiagnostics(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	d := n.device("dev-a", "192.168.0.10")
	var r diagnosticsResult
	n.json(&r, "-i", "pn0", "diagnostics", "-s", "dev-a")
	if len(r.Entries) != 0 {
		t.Fatalf("diagnosis %+v", r)
	}
	if out := n.ok("-i", "pn0", "diag", "-s", "dev-a"); out != "dev-a: no diagnosis\n" {
		t.Fatalf("text %q", out)
	}
	ctx := context.Background()
	addDiag(t, d, ctx, 1, 1, pnio.DiagnosisItem{ChannelNumber: 3, ChannelProperties: pnio.ChanDirInput | pnio.ChanMaintRequired, ErrorType: pnio.ChannelErrLineBreak})
	addDiag(t, d, ctx, 3, 1, pnio.DiagnosisItem{ChannelNumber: 0x8000, ChannelProperties: pnio.ChanDirOutput, ErrorType: 0x0200, ExtErrorType: 7, ExtAddValue: 9})
	n.json(&r, "-i", "pn0", "diagnostics", "-s", "dev-a")
	if len(r.Entries) != 2 {
		t.Fatalf("diagnosis %+v", r)
	}
	e := r.Entries[0]
	if e.Slot != 1 || e.Channel != 3 || e.Direction != "input" || e.Error != "line break" || e.Severity != "maintenance required" {
		t.Fatalf("entry %+v", e)
	}
	out := n.ok("-i", "pn0", "diagnostics", "-s", "dev-a")
	if !strings.Contains(out, "slot 1 subslot 0x0001 channel 0x0003 input: line break (0x0006), maintenance required") ||
		!strings.Contains(out, "manufacturer specific (0x0200), fault, extended 0x0007 value 0x00000009") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("-i", "pn0", "diagnostics", "-s", "dev-a", "-o", "table"); !strings.Contains(out, "line break") {
		t.Fatalf("table:\n%s", out)
	}
	n.json(&r, "-i", "pn0", "diagnostics", "-s", "dev-a", "--slot", "3", "--subslot", "1")
	if len(r.Entries) != 1 || r.Entries[0].Direction != "output" {
		t.Fatalf("submodule %+v", r)
	}
	// Names of severities, directions and error types.
	for p, want := range map[uint16]string{pnio.ChanMaintDemanded: "maintenance demanded", pnio.ChanMaintQualified: "qualified", 0: "fault"} {
		if severity(p) != want {
			t.Errorf("severity 0x%04X = %q", p, severity(p))
		}
	}
	if direction(pnio.ChanDirInOut) != "input/output" || direction(0) != "manufacturer" || errorName(0x0050) != "reserved" {
		t.Error("names")
	}
	views := diagViews([]pnio.DiagnosisData{{Slot: 4, USI: 0x1234, Data: []byte{1, 2}}})
	if len(views) != 1 || views[0].VendorData != "0102" || views[0].Error != "manufacturer specific" {
		t.Fatalf("vendor diagnosis %+v", views)
	}
}

func TestDeviceInfoAndStatus(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	d := n.device("dev-a", "192.168.0.10")
	n.device("dev-b", "192.168.0.11")
	var r deviceInfoResult
	n.json(&r, "-i", "pn0", "device", "info", "-s", "dev-a")
	if r.IM0 == nil || r.IM0.SerialNumber != "0042" || len(r.Modules) != 4 || r.Modules[0].Slot != 0 || len(r.Modules[0].Submodules) != 3 ||
		r.Modules[3].IdentNumber != 0x30 || len(r.Errors) != 0 {
		t.Fatalf("info %+v", r)
	}
	out := n.ok("-i", "pn0", "device", "info", "-s", "dev-a")
	for _, want := range []string{"Order ID:         OTF-CTL-TEST", "Real configuration:", "slot 3   module 0x00000030", "subslot 0x8001 submodule 0x00008001"} {
		if !strings.Contains(out, want) {
			t.Fatalf("text lacks %q:\n%s", want, out)
		}
	}
	if out := n.ok("-i", "pn0", "device", "info", "-s", "dev-a", "-o", "table"); strings.Count(out, "\n") != 8 {
		t.Fatalf("table:\n%s", out)
	}
	addDiag(t, d, context.Background(), 1, 1, pnio.DiagnosisItem{ChannelNumber: 1, ErrorType: pnio.ChannelErrShortCircuit})
	time.Sleep(300 * time.Millisecond) // LLDP between dev-a and dev-b
	var st statusResult
	n.json(&st, "-i", "pn0", "status", "-s", "dev-a")
	if !st.DCP || !st.RPC || st.Diagnoses["fault"] != 1 || len(st.Ports) != 1 || st.Ports[0].Link != "up" || !strings.Contains(st.Ports[0].Peer, "dev-b") {
		t.Fatalf("status %+v", st)
	}
	out = n.ok("-i", "pn0", "status", "-s", "dev-a")
	if !strings.Contains(out, "DCP: ok  RPC: ok") || !strings.Contains(out, "Diagnosis: 1 fault") || !strings.Contains(out, "Port 0x8001 port-001: link up, peer dev-b") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("-i", "pn0", "status", "-s", "dev-b", "-o", "table"); !strings.Contains(out, "port-001:up") {
		t.Fatalf("table:\n%s", out)
	}
	if out := n.ok("-i", "pn0", "status", "-s", "dev-b"); !strings.Contains(out, "Diagnosis: none") {
		t.Fatalf("text:\n%s", out)
	}
	if linkName(pnio.LinkStateDown) != "down" || linkName(7) != "state 7" {
		t.Error("link names")
	}
}

func TestLLDPAndTopology(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	n.device("dev-b", "192.168.0.11")
	n.device("dev-c", "")
	var r neighborsResult
	n.json(&r, "-i", "pn0", "lldp", "neighbors", "--wait", "400ms")
	if len(r.Neighbors) != 3 || r.Interface != "pn0" {
		t.Fatalf("neighbors %+v", r)
	}
	var a neighborView
	for _, nb := range r.Neighbors {
		if nb.ChassisID == "dev-a" {
			a = nb
		}
	}
	if a.PortID != "port-001.dev-a" || a.ManagementAddress != "192.168.0.10" || a.TTL == 0 || a.Alias == "" {
		t.Fatalf("dev-a %+v", a)
	}
	out := n.ok("-i", "pn0", "lldp", "neighbors", "--wait", "400ms")
	if !strings.Contains(out, `chassis "dev-a" port "port-001.dev-a"`) || !strings.Contains(out, "3 neighbor(s) on pn0") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("-i", "pn0", "lldp", "neighbors", "--wait", "400ms", "-o", "table"); !strings.Contains(out, "192.168.0.11") {
		t.Fatalf("table:\n%s", out)
	}
	// The topology: dev-a and dev-b are read, dev-c has no address.
	var tr topologyResult
	out, _, code := n.run("-i", "pn0", "topology", "--wait", "300ms", "--lldp-wait", "300ms", "--format", "json")
	if code != 7 {
		t.Fatalf("topology exit %d:\n%s", code, out)
	}
	decode(t, out, &tr)
	if len(tr.Nodes) != 3 || len(tr.Host) != 3 {
		t.Fatalf("topology %+v", tr)
	}
	byName := map[string]topoNode{}
	for _, nd := range tr.Nodes {
		byName[nd.Station] = nd
	}
	if !strings.Contains(byName["dev-c"].Error, "no IP address") || len(byName["dev-a"].Ports) != 1 || len(byName["dev-a"].Ports[0].Peers) != 2 || byName["dev-a"].Ports[0].Peers[0].Station == "" {
		t.Fatalf("nodes %+v", tr.Nodes)
	}
	if len(tr.Links) == 0 {
		t.Fatalf("links %+v", tr.Links)
	}
	out, _, _ = n.run("-i", "pn0", "topology", "--wait", "300ms")
	if !strings.Contains(out, "dev-a (") || !strings.Contains(out, "Links:") || !strings.Contains(out, "error: no IP address") {
		t.Fatalf("text:\n%s", out)
	}
	if out, _, _ := n.run("-i", "pn0", "topology", "--wait", "300ms", "-o", "table"); !strings.Contains(out, "PEERS") {
		t.Fatalf("table:\n%s", out)
	}
	// Peer names.
	if peerStation(pnio.PDPortPeer{PortID: "port-002.sw-1"}) != "sw-1" || peerPort(pnio.PDPortPeer{PortID: "port-002.sw-1"}) != "port-002" ||
		peerStation(pnio.PDPortPeer{PortID: "eth3"}) != "" || peerPort(pnio.PDPortPeer{PortID: "eth3"}) != "eth3" ||
		peerStation(pnio.PDPortPeer{PortID: "eth3", ChassisID: "switch-7"}) != "switch-7" ||
		peerStation(pnio.PDPortPeer{PortID: "port-001.io-1", ChassisID: "a system description"}) != "io-1" {
		t.Error("peer names")
	}
	if l := links([]topoNode{{Station: "a", Ports: []topoPort{{PortID: "p1", Peers: []topoPeer{{Station: "b", Port: "p2"}}}}}, {Station: "b", Ports: []topoPort{{PortID: "p2", Peers: []topoPeer{{Station: "a", Port: "p1"}, {Port: "x"}, {Station: "b", Port: "p2"}}}, {PortID: "p3"}}}}); len(l) != 1 {
		t.Fatalf("links %+v", l)
	}
}

// addDiag records a diagnosis; without an AR the device reports that no
// alarm could be sent.
func addDiag(t *testing.T, d *testDev, ctx context.Context, slot, subslot uint16, item pnio.DiagnosisItem) {
	t.Helper()
	if err := d.dev.AddDiagnosis(ctx, slot, subslot, item); err != nil && !errors.Is(err, profinet.ErrInvalidState) {
		t.Fatal(err)
	}
}
