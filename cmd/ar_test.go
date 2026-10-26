// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-profinet/controller"
	"github.com/otfabric/go-profinet/device"
	"github.com/otfabric/go-profinet/pnio"
	"github.com/otfabric/go-profinet/rpc"
)

func TestGSDMLCommands(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	dir := t.TempDir()
	mf := modelFile(t)
	// Generate to a directory (conventional name), a file and stdout.
	var g generateResult
	n.json(&g, "gsdml", "generate", "--model", mf, "-O", dir)
	if !strings.HasPrefix(filepath.Base(g.File), "GSDML-V2.35-") || g.Bytes == 0 {
		t.Fatalf("generate %+v", g)
	}
	file := filepath.Join(dir, "dev.xml")
	if out := n.ok("gsdml", "generate", "--model", mf, "-O", file); !strings.Contains(out, "wrote "+file) {
		t.Fatalf("text %q", out)
	}
	if out := n.ok("gsdml", "generate", "--model", mf); !strings.HasPrefix(out, "<?xml") {
		t.Fatalf("stdout %q", out[:40])
	}
	if out := n.ok("gsdml", "generate", "--model", mf, "-O", file, "-o", "table"); !strings.Contains(out, "BYTES") {
		t.Fatalf("table %q", out)
	}
	// Inspect.
	var in inspectResult
	n.json(&in, "gsdml", "inspect", file)
	if in.SchemaVersion != "2.35" || in.VendorID != "0xF000" || in.VendorName != "OT Fabric" || !in.Valid || len(in.DAPs) != 1 ||
		in.DAPs[0].Ports != 1 || in.DAPs[0].PlugSlots != 3 || in.DAPs[0].Modules != 4 || !in.DAPs[0].DeviceAccess {
		t.Fatalf("inspect %+v", in)
	}
	out := n.ok("gsdml", "inspect", file)
	if !strings.Contains(out, "Schema:   GSDML 2.35") || !strings.Contains(out, "Valid:    yes") || !strings.Contains(out, "3 plugged slot(s), 4 usable module(s)") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("gsdml", "inspect", file, "-o", "table"); !strings.Contains(out, "DAP") {
		t.Fatalf("table:\n%s", out)
	}
	// Validate: valid, and a file with issues (exit code 7).
	var v validateResult
	n.json(&v, "gsdml", "validate", file)
	if !v.Valid || len(v.Issues) != 0 || !strings.Contains(v.Scope, "not PI's GSDML checker") {
		t.Fatalf("validate %+v", v)
	}
	b, _ := os.ReadFile(file)
	bad := writeFile(t, "bad.xml", []byte(strings.Replace(string(b), `VendorID="0xF000"`, `VendorID="vendor"`, 1)))
	out, _, code := n.run("gsdml", "validate", bad)
	if code != 7 || !strings.Contains(out, "issue(s)") || !strings.Contains(out, "VendorID") {
		t.Fatalf("invalid file %d:\n%s", code, out)
	}
	if out, _, _ := n.run("gsdml", "validate", bad, "-o", "table"); !strings.Contains(out, "PATH") {
		t.Fatalf("table:\n%s", out)
	}
	if out, _, code := n.run("gsdml", "inspect", bad); code != 0 || !strings.Contains(out, "Valid:    no") {
		t.Fatalf("inspect invalid %d:\n%s", code, out)
	}
	// Modules and submodules.
	var mods modulesResult
	n.json(&mods, "gsdml", "modules", file)
	if len(mods.Modules) != 4 {
		t.Fatalf("modules %+v", mods)
	}
	out = n.ok("gsdml", "modules", file)
	if !strings.Contains(out, "0x00000040") || !strings.Contains(out, "4 module(s)") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("gsdml", "modules", file, "-o", "table"); !strings.Contains(out, "SUBMODULES") {
		t.Fatalf("table:\n%s", out)
	}
	var subs submodulesResult
	n.json(&subs, "gsdml", "submodules", file)
	if len(subs.Submodules) != 7 {
		t.Fatalf("submodules %+v", subs)
	}
	n.json(&subs, "gsdml", "submodules", file, "--module", "0x20")
	if len(subs.Submodules) != 1 || len(subs.Submodules[0].Records) != 1 || subs.Submodules[0].OutputLen != 4 {
		t.Fatalf("module 0x20 %+v", subs)
	}
	if out := n.ok("gsdml", "submodules", file, "--module", "0x20"); !strings.Contains(out, `record 0x0010 "Mode", 2 octets, default 0001`) {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("gsdml", "submodules", file, "-o", "table"); !strings.Contains(out, "RECORDS") {
		t.Fatalf("table:\n%s", out)
	}
	// Errors.
	n.fails(8, "invalid_input", "gsdml", "submodules", file, "--module", "0x99")
	n.fails(3, "invalid_input", "gsdml", "submodules", file, "--module", "x")
	n.fails(3, "invalid_gsdml", "gsdml", "modules", file, "--dap", "NoSuchDAP")
	n.fails(3, "input_file_open_failed", "gsdml", "inspect", file+".none")
	n.fails(3, "invalid_gsdml", "gsdml", "inspect", writeFile(t, "x.xml", []byte("<nope/>")))
	n.fails(3, "invalid_gsdml", "gsdml", "validate", writeFile(t, "y.xml", []byte("not xml")))
	n.fails(2, "invalid_input", "gsdml", "generate")
	n.fails(3, "invalid_model", "gsdml", "generate", "--model", writeFile(t, "m.json", []byte("{")))
	n.fails(3, "invalid_model", "gsdml", "generate", "--model", writeFile(t, "m2.json", []byte(`{"identity":{}}`)))
	n.fails(3, "input_file_open_failed", "gsdml", "generate", "--model", mf+".none")
	n.fails(10, "output_file_write_failed", "gsdml", "generate", "--model", mf, "-O", filepath.Join(dir, "no", "x.xml"))
}

func TestConnect(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	d := n.device("dev-a", "192.168.0.10")
	gf := gsdmlFile(t)
	// A diagnosis alarm while the AR runs.
	go diagnoseWhenRunning(d, 1, pnio.DiagnosisItem{ChannelNumber: 1, ChannelProperties: pnio.ChanDirInput, ErrorType: pnio.ChannelErrOverload})
	// The outputs reach the device while the AR runs.
	gotOutput := make(chan string, 1)
	go func() {
		for i := 0; i < 300; i++ {
			time.Sleep(10 * time.Millisecond)
			if v, iops, ok := d.dev.Output(2, 1); ok && iops == pnio.IOxSGood {
				gotOutput <- hex.EncodeToString(v)
				return
			}
		}
		gotOutput <- ""
	}()
	out := n.ok("-i", "pn0", "connect", "-s", "dev-a", "--gsdml", gf, "--output", "2:1:a1b2c3d4", "--output", "3:1:0102",
		"--param", "2:1:0x10:0003", "--duration", "3s", "--format", "json")
	if v := <-gotOutput; v != "a1b2c3d4" {
		t.Fatalf("output at the device %q", v)
	}
	evs := jsonLines(t, out)
	var running, alarm bool
	var sum map[string]any
	for _, e := range evs {
		switch e["event"] {
		case "state":
			if e["to"] == "Running" {
				running = true
			}
		case "alarm":
			al := e["alarm"].(map[string]any)
			alarm = al["type"] == "Diagnosis" && len(al["diagnosis"].([]any)) == 1
		case "summary":
			sum = e
		}
	}
	if !running || !alarm || sum == nil || sum["state"] != "Running" {
		t.Fatalf("events:\n%s", out)
	}
	stats := sum["stats"].(map[string]any)
	if stats["frames_received"].(float64) < 50 || stats["watchdog_expiries"] != 0.0 {
		t.Fatalf("stats %v", stats)
	}
	// Text output; the AR is released at the end.
	out = n.ok("-i", "pn0", "connect", "-s", "dev-a", "--model", modelFile(t), "--duration", "500ms")
	if !strings.Contains(out, "state Connecting → Parameterizing") || !strings.Contains(out, "summary: state Running") || !strings.Contains(out, "input slot 1 subslot 0x0001") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("-i", "pn0", "connect", "-s", "dev-a", "--model", modelFile(t), "--duration", "300ms", "-o", "table"); !strings.Contains(out, "summary") {
		t.Fatalf("table:\n%s", out)
	}
	eventually(t, "AR released", func() bool { return len(d.dev.ARs()) == 0 })
	// Interrupted by the user: a normal end.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Once the AR runs (t.Fatal must not be called here).
		defer cancel()
		for i := 0; i < 1000; i++ {
			for _, a := range d.dev.ARs() {
				if a.State == device.ARStateData {
					time.Sleep(200 * time.Millisecond)
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	var o, e strings.Builder
	if code := Run(ctx, n, []string{"-i", "pn0", "connect", "-s", "dev-a", "--gsdml", gf}, &o, &e); code != 0 || !strings.Contains(o.String(), "summary") {
		t.Fatalf("interrupted: %d\n%s\n%s", code, o.String(), e.String())
	}
}

func TestConnectErrors(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	gf, mf := gsdmlFile(t), modelFile(t)
	base := []string{"-i", "pn0", "connect", "-s", "dev-a", "--gsdml", gf, "--duration", "100ms"}
	n.fails(2, "invalid_input", "-i", "pn0", "connect", "--gsdml", gf)
	n.fails(3, "invalid_input", "-i", "pn0", "connect", "-s", "BAD", "--gsdml", gf)
	n.fails(2, "invalid_input", "-i", "pn0", "connect", "-s", "dev-a")
	n.fails(2, "invalid_flag_combination", "-i", "pn0", "connect", "-s", "dev-a", "--gsdml", gf, "--model", mf)
	for _, bad := range [][]string{
		{"--output", "2:1"}, {"--output", "x:1:00"}, {"--output", "1:1:00"}, {"--output", "2:1:00"},
		{"--param", "2:1:16"}, {"--param", "2:1:x:00"}, {"--assign-ip", "nope"},
		{"--plug", "x"}, {"--plug", "4=x"}, {"--plug", "4=0x99"},
	} {
		n.fails(3, "", append(append([]string{}, base...), bad...)...)
	}
	// A module the device does not have: the AR runs, the difference is reported.
	out, errOut, code := n.run(append(append([]string{}, base...), "--plug", "3=0x40", "--format", "json")...)
	if code != 0 || !strings.Contains(out, `"event":"module-diff"`) || !strings.Contains(errOut, "warning: the real configuration differs from the expected one: slot 3: wrong module 0x00000030") {
		t.Fatalf("module diff %d\n%s\n%s", code, out, errOut)
	}
	// No such device.
	n.fails(8, "", "-i", "pn0", "connect", "-s", "nobody", "--gsdml", gf, "--connect-timeout", "1500ms")
	// A bad model file.
	n.fails(3, "invalid_model", "-i", "pn0", "connect", "-s", "dev-a", "--model", writeFile(t, "m.json", []byte(`{"identity":{}}`)))
	n.fails(3, "invalid_model", "-i", "pn0", "connect", "-s", "dev-a", "--model", writeFile(t, "m2.json", []byte(`[`)))
	n.fails(3, "input_file_open_failed", "-i", "pn0", "connect", "-s", "dev-a", "--model", mf+".none")
}

func TestPlug(t *testing.T) {
	t.Parallel()
	m := testModel()
	if err := plug(m, []string{"4=0x40", "1=0x40"}); err != nil {
		t.Fatal(err)
	}
	if len(m.Slots) != 4 || m.Slots[0].Module.IdentNumber != 0x40 || m.Slots[3].Number != 4 {
		t.Fatalf("slots %+v", m.Slots)
	}
	if err := plug(m, []string{"0=0x40"}); err == nil {
		t.Fatal("slot 0 plugged")
	}
}

func TestARAndIOCR(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	gf := gsdmlFile(t)
	var r arInfoResult
	n.json(&r, "-i", "pn0", "ar", "-s", "dev-a", "--gsdml", gf, "--reduction-ratio", "8")
	if r.Station != "dev-a" || r.AR.UUID == "" || r.AR.Input.CycleMS != 8 || r.AR.Input.FrameID < 0xC000 || r.AR.Output.FrameID == 0 || r.AR.Type != "IOCARSingle" {
		t.Fatalf("ar %+v", r)
	}
	out := n.ok("-i", "pn0", "ar", "-s", "dev-a", "--gsdml", gf, "--advanced")
	if !strings.Contains(out, "Startup:     advanced, RT class 2") || !strings.Contains(out, "Input CR:") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("-i", "pn0", "ar", "-s", "dev-a", "--gsdml", gf, "-o", "table"); !strings.Contains(out, "FRAMEID") {
		t.Fatalf("table:\n%s", out)
	}
	var io iocrResult
	n.json(&io, "-i", "pn0", "iocr", "-s", "dev-a", "--gsdml", gf)
	data, iocs := 0, 0
	for _, e := range io.Entries {
		if e.Kind == "data" {
			data++
		} else {
			iocs++
		}
	}
	// Input CR: DAP 3 + slot 1 + slot 3 data, IOCS of slots 2 and 3; output CR: slots 2 and 3, IOCS of the rest.
	if data != 7 || iocs != 7 {
		t.Fatalf("iocr %d data %d iocs: %+v", data, iocs, io)
	}
	if out := n.ok("-i", "pn0", "iocr", "-s", "dev-a", "--gsdml", gf); !strings.Contains(out, "Input CR, FrameID") || !strings.Contains(out, "IOCS") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("-i", "pn0", "iocr", "-s", "dev-a", "--gsdml", gf, "-o", "table"); !strings.Contains(out, "OFFSET") {
		t.Fatalf("table:\n%s", out)
	}
	// ModuleDiff in the text of ar.
	diff := &pnio.ModuleDiffBlock{APIs: []pnio.DiffAPI{{Modules: []pnio.DiffModule{{Slot: 1, IdentNumber: 2, State: pnio.ModuleStateWrong, Submodules: []pnio.DiffSubmodule{{Subslot: 1}}}}}}}
	r = arInfoResult{Diffs: differences(diff), ModuleDiff: diffView(diff)}
	if s, _ := r.MarshalTextOutput(); !strings.Contains(s, "ModuleDiff:") || !strings.Contains(s, "slot 1: wrong module") || len(r.ModuleDiff[0].Submodules) != 1 {
		t.Fatalf("diff text:\n%s", s)
	}
	if diffView(nil) != nil {
		t.Fatal("nil diff view")
	}
}

func TestIO(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	d := n.device("dev-a", "192.168.0.10")
	gf := gsdmlFile(t)
	_ = d.dev.SetInput(1, 1, []byte{1, 2, 3, 4, 5, 6, 7, 8}, pnio.IOxSGood)
	_ = d.dev.SetInput(3, 1, []byte{9, 9}, pnio.IOxSGood)
	var st ioStatusResult
	n.json(&st, "-i", "pn0", "io", "status", "-s", "dev-a", "--gsdml", gf, "--output", "2:1:00000001")
	var in1 inputValue
	for _, in := range st.Inputs {
		if in.Slot == 1 {
			in1 = in
		}
	}
	if st.State != "Running" || in1.Data != "0102030405060708" || in1.IOPS != "good" || len(st.Outputs) != 2 {
		t.Fatalf("status %+v", st)
	}
	var good, bad int
	for _, o := range st.Outputs {
		if o.IOCS == "good" {
			good++
		} else {
			bad++
		}
	}
	if good != 2 {
		t.Fatalf("outputs %+v", st.Outputs)
	}
	out := n.ok("-i", "pn0", "io", "status", "-s", "dev-a", "--gsdml", gf, "--settle", "100ms")
	if !strings.Contains(out, "input  slot 1 subslot 0x0001: 0102030405060708 IOPS good") || !strings.Contains(out, "output slot 2 subslot 0x0001: IOCS") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("-i", "pn0", "io", "status", "-s", "dev-a", "--gsdml", gf, "--settle", "100ms", "-o", "table"); !strings.Contains(out, "DIRECTION") {
		t.Fatalf("table:\n%s", out)
	}
	// Monitor: samples at the interval; with --changes only when the inputs change.
	out = n.ok("-i", "pn0", "io", "monitor", "-s", "dev-a", "--gsdml", gf, "--interval", "100ms", "--duration", "700ms", "--format", "json")
	samples := 0
	for _, e := range jsonLines(t, out) {
		if e["event"] == "sample" {
			samples++
		}
	}
	if samples < 3 {
		t.Fatalf("%d samples:\n%s", samples, out)
	}
	out = n.ok("-i", "pn0", "io", "monitor", "-s", "dev-a", "--gsdml", gf, "--interval", "100ms", "--duration", "700ms", "--changes", "--format", "json")
	samples = 0
	for _, e := range jsonLines(t, out) {
		if e["event"] == "sample" {
			samples++
		}
	}
	if samples < 1 || samples > 3 {
		t.Fatalf("%d samples with --changes:\n%s", samples, out)
	}
	if out := n.ok("-i", "pn0", "io", "monitor", "-s", "dev-a", "--gsdml", gf, "--interval", "100ms", "--duration", "300ms"); !strings.Contains(out, "1/0x0001=0102030405060708") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("-i", "pn0", "io", "monitor", "-s", "dev-a", "--gsdml", gf, "--interval", "100ms", "--duration", "300ms", "-o", "table"); !strings.Contains(out, "TIME") {
		t.Fatalf("table:\n%s", out)
	}
	// Statistics.
	var s statisticsResult
	n.json(&s, "-i", "pn0", "io", "statistics", "-s", "dev-a", "--gsdml", gf, "--duration", "500ms", "--reduction-ratio", "2", "--watchdog-factor", "50", "--data-hold-factor", "50")
	if s.State != "Running" || s.Cycle != "2ms" || s.Stats.Received < 100 || s.FrameRate < 100 || s.Stats.Connects < 1 {
		t.Fatalf("statistics %+v", s)
	}
	if out := n.ok("-i", "pn0", "io", "statistics", "-s", "dev-a", "--gsdml", gf, "--duration", "200ms"); !strings.Contains(out, "provider:") || !strings.Contains(out, "scheduler:") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("-i", "pn0", "io", "statistics", "-s", "dev-a", "--gsdml", gf, "--duration", "200ms", "-o", "table"); !strings.Contains(out, "frames received") {
		t.Fatalf("table:\n%s", out)
	}
	if ioxs(0, true) != "bad (0x00)" || ioxs(0, false) != "-" {
		t.Error("ioxs")
	}
}

func TestAlarms(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	d := n.device("dev-a", "192.168.0.10")
	go diagnoseWhenRunning(d, 3, pnio.DiagnosisItem{ChannelNumber: 2, ChannelProperties: pnio.ChanDirOutput, ErrorType: pnio.ChannelErrShortCircuit})
	out := n.ok("-i", "pn0", "alarms", "-s", "dev-a", "--gsdml", gsdmlFile(t), "--duration", "3s")
	if !strings.Contains(out, "alarm Diagnosis (low) slot 3 subslot 0x0001") || !strings.Contains(out, "short circuit") || strings.Contains(out, "state ") {
		t.Fatalf("alarms:\n%s", out)
	}
	e := arEvent{Time: "t", Event: "alarm", Alarm: &alarmView{Type: "Process", Priority: "high", Data: "01"}}
	if s, _ := e.MarshalTextOutput(); !strings.Contains(s, "data 01") {
		t.Fatalf("process alarm text %q", s)
	}
	if rows := e.TableRows(); len(rows) != 1 || rows[0][1] != "alarm" {
		t.Fatalf("rows %v", rows)
	}
	if s, _ := (arEvent{Time: "t", Event: "other", Error: "x"}).MarshalTextOutput(); s != "t other x\n" {
		t.Fatalf("other %q", s)
	}
}

func TestDisconnect(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	d := n.device("dev-a", "192.168.0.10")
	// An AR left behind: a device access AR of another tool.
	_, _, st, mac := n.endpoint("192.168.0.3")
	conn, err := st.ListenPacket(context.Background(), "udp4", ":0")
	if err != nil {
		t.Fatal(err)
	}
	cli := rpc.NewClient(conn, true, rpc.ClientOptions{})
	defer func() { _ = cli.Close() }()
	rc := controller.NewRecordClient(cli, &net.UDPAddr{IP: net.IPv4(192, 168, 0, 10), Port: rpc.Port}, d.dev.ObjectUUID())
	ar, err := rc.OpenSupervisor(context.Background(), controller.SupervisorConfig{InitiatorMAC: mac, InitiatorObject: pnio.ObjectUUID(1, 2, 3), ActivityTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.dev.ARs()) != 1 {
		t.Fatal("no AR")
	}
	n.fails(6, "pnio_error", "-i", "pn0", "disconnect", "-s", "dev-a", "--ar", ar.UUID().String(), "--session-key", "999")
	var r disconnectResult
	n.json(&r, "-i", "pn0", "disconnect", "-s", "dev-a", "--ar", ar.UUID().String(), "--session-key", strconv.Itoa(int(ar.SessionKey())))
	if r.AR != ar.UUID().String() {
		t.Fatalf("disconnect %+v", r)
	}
	eventually(t, "AR closed", func() bool { return len(d.dev.ARs()) == 0 })
	if s, _ := r.MarshalTextOutput(); !strings.Contains(s, "released AR") {
		t.Fatalf("text %q", s)
	}
	if rows := r.TableRows(); rows[0][0] != "dev-a" {
		t.Fatalf("rows %v", rows)
	}
	n.fails(3, "invalid_input", "-i", "pn0", "disconnect", "-s", "dev-a", "--ar", "nope")
	n.fails(2, "invalid_input", "-i", "pn0", "disconnect", "-s", "dev-a")
}

// diagnoseWhenRunning raises a diagnosis of slot/1 once the device has an
// IOCAR in the Data state.
func diagnoseWhenRunning(d *testDev, slot uint16, item pnio.DiagnosisItem) {
	for i := 0; i < 1000; i++ {
		for _, a := range d.dev.ARs() {
			if a.Type == pnio.ARTypeIOCARSingle && a.State == device.ARStateData {
				_ = d.dev.AddDiagnosis(context.Background(), slot, 1, item)
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
