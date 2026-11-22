// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-profinet/controller"
	"github.com/otfabric/go-profinet/dcp"
	"github.com/otfabric/go-profinet/device"
	"github.com/otfabric/go-profinet/model"
	"github.com/otfabric/go-profinet/pnio"
)

// checkJSON is the JSON of gsdml check, as a consumer reads it.
type checkJSON struct {
	SchemaVersion                 string `json:"schema_version"`
	Errors, Warnings, Unvalidated int
	Issues                        []struct {
		Path, Message, Severity, Rule string
		Line                          int
	}
	Scope string
}

func TestGSDMLCheck(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	file := filepath.Join(t.TempDir(), "dev.xml")
	n.ok("gsdml", "generate", "--model", modelFile(t), "-O", file)
	var r checkJSON
	n.json(&r, "gsdml", "check", file)
	if r.Errors != 0 || r.SchemaVersion != "2.35" || !strings.Contains(r.Scope, "not PI's GSDML checker") {
		t.Fatalf("check %+v", r)
	}
	out := n.ok("gsdml", "check", file)
	if !strings.Contains(out, "0 error(s)") || !strings.Contains(out, "checked: ") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("gsdml", "check", file, "-o", "table"); !strings.Contains(out, "SEVERITY") && !strings.Contains(out, "0 error") {
		t.Fatalf("table:\n%s", out)
	}
	// An element outside the schema: an error of the schema, with its
	// line; against the catalog, unvalidated, an issue under --strict.
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	odd := bytes.Replace(b, []byte("</DeviceIdentity>"), []byte("<FutureThing/></DeviceIdentity>"), 1)
	oddFile := writeFile(t, "odd.xml", odd)
	out, _, code := n.run("gsdml", "check", oddFile, "--format", "json")
	decode(t, out, &r)
	if code != 7 || r.Errors != 1 || r.Issues[0].Rule != "schema" || r.Issues[0].Line == 0 || !strings.Contains(r.Scope, "schema of version 2.35") {
		t.Fatalf("odd: exit %d %+v", code, r)
	}
	if out, _, _ := n.run("gsdml", "check", oddFile); !strings.Contains(out, "line ") {
		t.Fatalf("odd text:\n%s", out)
	}
	if out, _, _ := n.run("gsdml", "check", oddFile, "-o", "table"); !strings.Contains(out, "LINE") {
		t.Fatalf("odd table:\n%s", out)
	}
	n.json(&r, "gsdml", "check", oddFile, "--against", "catalog")
	if r.Unvalidated == 0 || r.Errors != 0 || r.Issues[0].Severity != "unvalidated" || r.Issues[0].Rule != "unknown" || !strings.Contains(r.Scope, "catalog") {
		t.Fatalf("odd against the catalog %+v", r)
	}
	if _, _, code := n.run("gsdml", "check", oddFile, "--against", "catalog", "--strict"); code != 7 {
		t.Fatalf("--strict exit %d", code)
	}
	n.fails(2, "invalid_input", "gsdml", "check", oddFile, "--against", "xsd")
	// An error: a reference to nothing.
	bad := bytes.Replace(b, []byte(`TextId="`), []byte(`TextId="nothing-`), 1)
	badFile := writeFile(t, "bad.xml", bad)
	out, _, code = n.run("gsdml", "check", badFile, "--format", "json")
	if code != 7 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	decode(t, out, &r)
	if r.Errors == 0 {
		t.Fatalf("bad %+v", r)
	}
}

func TestHelloListen(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	go func() {
		time.Sleep(300 * time.Millisecond)
		n.device("hello-dev", "192.168.0.30", func(c *device.Config) { c.Hello = dcp.HelloOnLinkUp })
	}()
	out := n.ok("-i", "pn0", "hello", "listen", "--duration", "1500ms", "--format", "json")
	evs := jsonLines(t, out)
	if len(evs) == 0 {
		t.Fatalf("no Hello:\n%s", out)
	}
	dev := evs[0]["device"].(map[string]any)
	if evs[0]["event"] != "hello" || dev["name_of_station"] != "hello-dev" || dev["ip"] != "192.168.0.30" {
		t.Fatalf("event %v", evs[0])
	}
	out = n.ok("-i", "pn0", "hello", "listen", "--duration", "200ms")
	_ = out
}

func TestTopologyCheck(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	n.device("dev-b", "192.168.0.11")
	time.Sleep(400 * time.Millisecond) // LLDP
	// An unknown port: the topology differs, exit code 7.
	out, _, code := n.run("-i", "pn0", "topology", "check", "--link", "dev-a/port-009=dev-b/port-001", "--format", "json")
	if code != 7 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	var r topoCheckResult
	decode(t, out, &r)
	if r.OK || len(r.Report.Links) != 1 || r.Report.Links[0].Verdict != "unknown port" {
		t.Fatalf("report %+v", r)
	}
	file := writeFile(t, "links.json", []byte(`[{"a":{"station":"dev-a","port":"port-001"},"b":{"station":"dev-b","port":"port-001"}}]`))
	out, _, _ = n.run("-i", "pn0", "topology", "check", "--expect", file)
	if !strings.Contains(out, "dev-a/port-001 — dev-b/port-001: ") || !strings.Contains(out, "topology ") || strings.Contains(out, "{") {
		t.Fatalf("text:\n%s", out)
	}
	// Every part of a report in text.
	txt, _ := topoCheckResult{Report: controller.TopologyReport{
		Links:       []controller.LinkResult{{Link: controller.Link{A: controller.PortRef{Station: "a", Port: "port-001"}, B: controller.PortRef{Station: "b", Port: "port-002"}}, Verdict: "wrong peer", Detail: "a sees c/port-001"}},
		Unexpected:  []controller.Link{{A: controller.PortRef{Station: "a", Port: "port-002"}, B: controller.PortRef{Station: "c", Port: "port-001"}}},
		Unreachable: map[string]string{"z": "timeout", "y": "no IP"},
	}}.MarshalTextOutput()
	want := "a/port-001 — b/port-002: wrong peer (a sees c/port-001)\na/port-002 — c/port-001: unexpected\ny: unreachable (no IP)\nz: unreachable (timeout)\ntopology differs\n"
	if txt != want {
		t.Fatalf("text %q, want %q", txt, want)
	}
	n.fails(2, "invalid_input", "-i", "pn0", "topology", "check")
	n.fails(3, "invalid_input", "-i", "pn0", "topology", "check", "--link", "dev-a=dev-b")
	n.fails(3, "invalid_input", "-i", "pn0", "topology", "check", "--expect", "/does/not/exist")
	n.fails(3, "invalid_input", "-i", "pn0", "topology", "check", "--expect", writeFile(t, "bad.json", []byte("{")))
}

func TestConnectSlotsAndPeerChecks(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	d := n.device("dev-a", "192.168.0.10")
	// Only slot 1 (shared device), and a peer check that cannot hold.
	out := n.ok("-i", "pn0", "connect", "-s", "dev-a", "--model", modelFile(t), "--slot", "1",
		"--peer-check", "1=switch-9/port-003", "--duration", "1500ms", "--format", "json")
	var sum map[string]any
	for _, e := range jsonLines(t, out) {
		if e["event"] == "summary" {
			sum = e
		}
	}
	if sum == nil || sum["state"] != "Running" {
		t.Fatalf("events:\n%s", out)
	}
	for _, in := range sum["inputs"].([]any) {
		if s := in.(map[string]any)["slot"].(float64); s != 0 && s != 1 {
			t.Fatalf("slot %v in a slot-1 AR:\n%s", s, out)
		}
	}
	var mismatch bool
	for _, it := range d.dev.Diagnoses(0, model.SubslotPort1) {
		mismatch = mismatch || it.ErrorType == pnio.ChannelErrRemoteMismat
	}
	if !mismatch {
		t.Fatal("the peer check was not written")
	}
	base := []string{"-i", "pn0", "connect", "-s", "dev-a", "--model", modelFile(t), "--duration", "100ms"}
	n.fails(3, "invalid_input", append(base, "--slot", "0")...)
	n.fails(3, "invalid_input", append(base, "--slot", "9")...)
	n.fails(3, "invalid_input", append(base, "--peer-check", "x=a/b")...)
	n.fails(3, "invalid_input", append(base, "--peer-check", "1=nobody")...)
	_ = context.Background
}

// TestGSDMLCheckModel: without catalog errors, gsdml check converts each
// device access point as --gsdml does. A Ref default go-profinet has no
// encoding for is unvalidated (rule model; exit 0, 7 with --strict), one
// that is wrong (a string longer than its Length) an error (exit 7); the
// same files given to --gsdml fail with unsupported_gsdml and
// invalid_gsdml.
func TestGSDMLCheckModel(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	file := filepath.Join(t.TempDir(), "dev.xml")
	n.ok("gsdml", "generate", "--model", modelFile(t), "-O", file)
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	const name = `<Name TextId="M2_SM1_R16"></Name>`
	if !bytes.Contains(b, []byte(name)) {
		t.Fatal("the generated GSDML no longer has record 16")
	}
	withRef := func(ref string) string {
		return writeFile(t, "ref.xml", bytes.Replace(b, []byte(name), []byte(name+ref), 1))
	}
	const dapPath = "ISO15745Profile/ProfileBody/ApplicationProcess/DeviceAccessPointList/DeviceAccessPointItem[DAP]"

	unsupported := withRef(`<Ref ByteOffset="0" DataType="Integer16_S" DefaultValue="1" TextId="M2_SM1_R16"/>`)
	var r checkJSON
	n.json(&r, "gsdml", "check", unsupported)
	// The field without a verified encoding (go-profinet's record rules),
	// and the DAP that does not convert because of it.
	if r.Errors != 0 || r.Unvalidated != 2 || len(r.Issues) != 2 || r.Issues[0].Rule != "parameter-encoding" || r.Issues[1].Rule != "model" ||
		r.Issues[1].Severity != "unvalidated" || r.Issues[1].Path != dapPath || !strings.Contains(r.Issues[1].Message, "Integer16_S") {
		t.Fatalf("unsupported %+v", r)
	}
	if _, _, code := n.run("gsdml", "check", unsupported, "--strict"); code != 7 {
		t.Fatalf("--strict exit %d", code)
	}
	n.fails(3, "unsupported_gsdml", "gsdml", "modules", unsupported, "--dap", "DAP")

	tooLong := withRef(`<Ref ByteOffset="0" DataType="VisibleString" Length="2" DefaultValue="abc" TextId="M2_SM1_R16"/>`)
	out, _, code := n.run("gsdml", "check", tooLong, "--format", "json")
	if code != 7 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	decode(t, out, &r)
	// go-profinet's record rules report the record: an error, so the
	// conversion is not tried.
	if r.Errors != 1 || r.Issues[0].Rule != "parameter-record" || r.Issues[0].Severity != "error" || !strings.Contains(r.Issues[0].Message, "exceeds its Length") {
		t.Fatalf("too long %+v", r)
	}
	n.fails(3, "invalid_gsdml", "gsdml", "modules", tooLong, "--dap", "DAP")
	if text := n.ok("gsdml", "check", unsupported); !strings.Contains(text, "does not convert to a model") {
		t.Fatalf("text:\n%s", text)
	}

	// With catalog errors the conversion is left out: they are reported.
	both := bytes.Replace(bytes.Replace(b, []byte(name), []byte(name+`<Ref ByteOffset="0" DataType="Integer16_S" DefaultValue="1" TextId="M2_SM1_R16"/>`), 1),
		[]byte(`TextId="T_DAP_Name"`), []byte(`TextId="nothing"`), 1)
	out, _, _ = n.run("gsdml", "check", writeFile(t, "both.xml", both), "--format", "json")
	decode(t, out, &r)
	for _, i := range r.Issues {
		if i.Rule == "model" {
			t.Fatalf("model issue next to catalog errors: %+v", r)
		}
	}

	// A document type declaration with an internal subset is unsupported.
	dtd := bytes.Replace(b, []byte("<ISO15745Profile"), []byte(`<!DOCTYPE ISO15745Profile [<!ENTITY e "x">]><ISO15745Profile`), 1)
	n.fails(3, "unsupported_gsdml", "gsdml", "check", writeFile(t, "dtd.xml", dtd))
}
