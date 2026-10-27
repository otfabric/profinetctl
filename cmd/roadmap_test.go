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

	"github.com/otfabric/go-profinet/dcp"
	"github.com/otfabric/go-profinet/device"
	"github.com/otfabric/go-profinet/model"
	"github.com/otfabric/go-profinet/pnio"
)

// checkJSON is the JSON of gsdml check, as a consumer reads it.
type checkJSON struct {
	SchemaVersion                 string `json:"schema_version"`
	Errors, Warnings, Unvalidated int
	Issues                        []struct{ Path, Message, Severity, Rule string }
	Scope                         string
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
	// An element outside the catalog: unvalidated, an issue under --strict.
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	odd := bytes.Replace(b, []byte("</DeviceIdentity>"), []byte("<FutureThing/></DeviceIdentity>"), 1)
	oddFile := writeFile(t, "odd.xml", odd)
	n.json(&r, "gsdml", "check", oddFile)
	if r.Unvalidated == 0 || r.Errors != 0 || r.Issues[0].Severity != "unvalidated" || r.Issues[0].Rule != "unknown" {
		t.Fatalf("odd %+v", r)
	}
	if _, _, code := n.run("gsdml", "check", oddFile, "--strict"); code != 7 {
		t.Fatalf("--strict exit %d", code)
	}
	// An error: a reference to nothing.
	bad := bytes.Replace(b, []byte(`TextId="`), []byte(`TextId="nothing-`), 1)
	badFile := writeFile(t, "bad.xml", bad)
	out, _, code := n.run("gsdml", "check", badFile, "--format", "json")
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
	if !strings.Contains(out, "dev-a") || !strings.Contains(out, "topology") {
		t.Fatalf("text:\n%s", out)
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
