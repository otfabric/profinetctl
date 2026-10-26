// SPDX-License-Identifier: MIT

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/go-profinet/dcp"
	"github.com/otfabric/go-profinet/pcap"
)

func TestInterfaces(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	out := n.ok("interfaces")
	if !strings.Contains(out, "pn0  02:00:00:00:00:01  mtu 1500  up  192.168.0.2/24") || !strings.Contains(out, "pn1") {
		t.Fatalf("text:\n%s", out)
	}
	var r interfacesResult
	n.json(&r, "interfaces")
	if r.Backend != "vnet" || len(r.Interfaces) != 2 || r.Interfaces[1].Up {
		t.Fatalf("json %+v", r)
	}
	if out := n.ok("interfaces", "-o", "table"); !strings.HasPrefix(out, "NAME") || !strings.Contains(out, "down") {
		t.Fatalf("table:\n%s", out)
	}
}

func TestDiscover(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	a := n.device("dev-a", "192.168.0.10")
	n.device("dev-b", "192.168.0.11")
	n.device("", "")
	var r discoverResult
	n.json(&r, "-i", "pn0", "discover", "--wait", "300ms")
	if len(r.Devices) != 3 || len(r.Duplicates) != 0 {
		t.Fatalf("devices %+v", r)
	}
	var found bool
	for _, d := range r.Devices {
		if d.NameOfStation == "dev-a" {
			found = true
			if d.MAC != a.mac.String() || d.IP != "192.168.0.10" || d.VendorID != 0xF000 || d.DeviceID != 0x0101 ||
				d.TypeOfStation != "profinetctl test device" || len(d.Roles) != 1 || d.Roles[0] != "IO-Device" || d.IPSetBy != "static" {
				t.Fatalf("dev-a %+v", d)
			}
		}
	}
	if !found {
		t.Fatal("dev-a not found")
	}
	// Filters.
	for _, f := range [][]string{{"--station", "dev-b"}, {"--ip", "192.168.0.11"}, {"--vendor", "0xF000", "--device", "0x0101"}} {
		var r discoverResult
		n.json(&r, append([]string{"-i", "pn0", "devices", "--wait", "300ms"}, f...)...)
		if f[0] == "--vendor" {
			if len(r.Devices) != 3 {
				t.Fatalf("%v: %+v", f, r)
			}
			continue
		}
		if len(r.Devices) != 1 || r.Devices[0].NameOfStation != "dev-b" {
			t.Fatalf("%v: %+v", f, r)
		}
	}
	// Text and table.
	out := n.ok("-i", "pn0", "discover", "--wait", "300ms")
	if !strings.Contains(out, "dev-a") || !strings.Contains(out, "3 device(s)") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("-i", "pn0", "discover", "--wait", "300ms", "-o", "table"); !strings.HasPrefix(out, "MAC") || strings.Count(out, "\n") != 5 {
		t.Fatalf("table:\n%s", out)
	}
	// Duplicate names are reported.
	n.device("dev-a", "192.168.0.12")
	n.json(&r, "-i", "pn0", "discover", "--wait", "300ms")
	if len(r.Duplicates) != 1 || r.Duplicates[0] != "dev-a" {
		t.Fatalf("duplicates %+v", r.Duplicates)
	}
	if out := n.ok("-i", "pn0", "discover", "--wait", "300ms"); !strings.Contains(out, `warning: station name "dev-a" is used by more than one device`) {
		t.Fatalf("text:\n%s", out)
	}
	n.fails(6, "duplicate_station_name", "-i", "pn0", "identify", "-s", "dev-a")
	// Bad filters.
	n.fails(3, "invalid_input", "-i", "pn0", "discover", "--ip", "nope")
	n.fails(3, "invalid_input", "-i", "pn0", "discover", "--vendor", "x")
}

func TestIdentify(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	d := n.device("dev-a", "192.168.0.10")
	for _, sel := range [][]string{{"-s", "dev-a"}, {"--mac", d.mac.String()}} {
		var r identifyResult
		n.json(&r, append([]string{"-i", "pn0", "identify"}, sel...)...)
		if r.Device.NameOfStation != "dev-a" || r.Device.MAC != d.mac.String() || r.Device.Instance == nil || len(r.Device.Options) == 0 {
			t.Fatalf("%v: %+v", sel, r.Device)
		}
	}
	out := n.ok("-i", "pn0", "identify", "-s", "dev-a")
	for _, want := range []string{"NameOfStation:    dev-a", "Vendor/Device ID: 0xF000 / 0x0101", "IP:               192.168.0.10 mask 255.255.255.0", "Options:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("text lacks %q:\n%s", want, out)
		}
	}
	if out := n.ok("-i", "pn0", "identify", "-s", "dev-a", "-o", "table"); !strings.Contains(out, "dev-a") {
		t.Fatalf("table:\n%s", out)
	}
	// Selection errors.
	n.fails(8, "device_not_found", "-i", "pn0", "identify", "-s", "nobody", "--wait", "200ms")
	n.fails(2, "invalid_input", "-i", "pn0", "identify")
	n.fails(2, "invalid_flag_combination", "-i", "pn0", "identify", "-s", "dev-a", "--mac", d.mac.String())
	n.fails(3, "invalid_input", "-i", "pn0", "identify", "-s", "Not_A_Name")
	n.fails(3, "invalid_input", "-i", "pn0", "identify", "--mac", "zz")
	n.fails(5, "request_timeout", "-i", "pn0", "identify", "--mac", "02:00:00:00:99:99", "--timeout", "200ms")
}

func TestNameAndIP(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	d := n.device("dev-a", "192.168.0.10")
	if out := n.ok("-i", "pn0", "name", "get", "--mac", d.mac.String()); out != "dev-a\n" {
		t.Fatalf("name get %q", out)
	}
	var ch changeResult
	n.json(&ch, "-i", "pn0", "name", "set", "renamed", "-s", "dev-a", "--permanent")
	if ch.Operation != "name set" || ch.Value != "renamed" || ch.Permanent == nil || !*ch.Permanent {
		t.Fatalf("set %+v", ch)
	}
	var nr nameResult
	n.json(&nr, "-i", "pn0", "name", "get", "-s", "renamed")
	if nr.Name != "renamed" {
		t.Fatalf("get %+v", nr)
	}
	if out := n.ok("-i", "pn0", "name", "set", "dev-a", "-s", "renamed"); !strings.Contains(out, "name set: "+d.mac.String()+" renamed → dev-a (temporary") {
		t.Fatalf("text %q", out)
	}
	if out := n.ok("-i", "pn0", "name", "get", "-s", "dev-a", "-o", "table"); !strings.Contains(out, "dev-a") {
		t.Fatalf("table %q", out)
	}
	n.fails(2, "invalid_input", "-i", "pn0", "name", "set", "-s", "dev-a")
	n.fails(2, "invalid_flag_combination", "-i", "pn0", "name", "set", "x", "--clear", "-s", "dev-a")
	n.fails(3, "invalid_input", "-i", "pn0", "name", "set", "Bad_Name", "-s", "dev-a")

	// IP.
	var ip ipResult
	n.json(&ip, "-i", "pn0", "ip", "get", "-s", "dev-a")
	if ip.IP != "192.168.0.10" || ip.Mask != "255.255.255.0" || ip.SetBy != "static" {
		t.Fatalf("ip %+v", ip)
	}
	n.json(&ch, "-i", "pn0", "ip", "set", "192.168.0.20/16", "--gateway", "192.168.0.1", "-s", "dev-a")
	if ch.Value != "192.168.0.20/16 via 192.168.0.1" {
		t.Fatalf("set %+v", ch)
	}
	n.json(&ip, "-i", "pn0", "ip", "get", "-s", "dev-a")
	if ip.IP != "192.168.0.20" || ip.Mask != "255.255.0.0" || ip.Gateway != "192.168.0.1" {
		t.Fatalf("ip after set %+v", ip)
	}
	if out := n.ok("-i", "pn0", "ip", "get", "-s", "dev-a"); out != "192.168.0.20 mask 255.255.0.0 gateway 192.168.0.1 (static)\n" {
		t.Fatalf("text %q", out)
	}
	if out := n.ok("-i", "pn0", "ip", "get", "-s", "dev-a", "-o", "table"); !strings.Contains(out, "192.168.0.20") {
		t.Fatalf("table %q", out)
	}
	n.json(&ch, "-i", "pn0", "ip", "set", "192.168.0.10", "-s", "dev-a")
	if ch.Value != "192.168.0.10/24" {
		t.Fatalf("default prefix %+v", ch)
	}
	n.json(&ch, "-i", "pn0", "ip", "set", "--clear", "-s", "dev-a")
	n.json(&ip, "-i", "pn0", "ip", "get", "-s", "dev-a")
	if ip.IP != "0.0.0.0" || ch.Operation != "ip cleared" {
		t.Fatalf("cleared %+v %+v", ip, ch)
	}
	n.json(&ch, "-i", "pn0", "name", "set", "--clear", "--mac", d.mac.String())
	n.json(&nr, "-i", "pn0", "name", "get", "--mac", d.mac.String())
	if nr.Name != "" || ch.Operation != "name cleared" {
		t.Fatalf("name cleared %+v %+v", nr, ch)
	}
	for _, bad := range [][]string{{"ip", "set", "300.1.1.1/24"}, {"ip", "set", "::1/64"}, {"ip", "set", "10.0.0.1/24", "--gateway", "x"}} {
		n.fails(3, "invalid_input", append([]string{"-i", "pn0", "--mac", d.mac.String()}, bad...)...)
	}
	n.fails(2, "invalid_input", "-i", "pn0", "ip", "set", "--mac", d.mac.String())
	n.fails(2, "invalid_flag_combination", "-i", "pn0", "ip", "set", "10.0.0.1/24", "--clear", "--mac", d.mac.String())
}

func TestSignalAndReset(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	d := n.device("dev-a", "192.168.0.10")
	if out := n.ok("-i", "pn0", "signal", "-s", "dev-a"); !strings.HasPrefix(out, "signal: ") {
		t.Fatalf("signal %q", out)
	}
	if out := n.ok("-i", "pn0", "signal", "-s", "dev-a", "-o", "table"); !strings.Contains(out, "signal") {
		t.Fatalf("table %q", out)
	}
	d.h.mu.Lock()
	signals := d.h.signals
	d.h.mu.Unlock()
	if signals != 2 {
		t.Fatalf("signals %d", signals)
	}
	// Reset needs a confirmation that names the target.
	n.fails(2, "confirmation_required", "-i", "pn0", "reset", "-s", "dev-a")
	msg := n.fails(2, "confirmation_required", "-i", "pn0", "reset", "-s", "dev-a", "--confirm", "dev-b")
	if !strings.Contains(msg, "names neither") {
		t.Fatalf("message %q", msg)
	}
	n.fails(3, "invalid_input", "-i", "pn0", "reset", "-s", "dev-a", "--mode", "everything", "--confirm", "dev-a")
	var ch changeResult
	n.json(&ch, "-i", "pn0", "reset", "-s", "dev-a", "--confirm", d.mac.String(), "--mode", "application")
	if ch.Operation != "reset" || ch.Value != "application" {
		t.Fatalf("reset %+v", ch)
	}
	n.ok("-i", "pn0", "reset", "--mac", d.mac.String(), "--confirm", "dev-a")
	d.h.mu.Lock()
	resets := append([]uint16(nil), d.h.resets...)
	d.h.mu.Unlock()
	if len(resets) != 2 || resets[0] != dcp.ResetApplicationData || resets[1] != dcp.ResetCommunication {
		t.Fatalf("resets %v", resets)
	}
}

func TestGlobalErrors(t *testing.T) {
	n := newTestNet(t)
	n.fails(2, "invalid_input", "discover")
	n.fails(3, "interface_open_failed", "-i", "eth9", "discover")
	n.openErr = os.ErrPermission
	msg := n.fails(4, "permission_denied", "-i", "pn0", "discover")
	if !strings.Contains(msg, "CAP_NET_RAW") {
		t.Fatalf("message %q", msg)
	}
	n.openErr = errors.New("device busy")
	n.fails(4, "interface_open_failed", "-i", "pn0", "discover")
	n.openErr = nil
	n.fails(3, "invalid_input", "-i", "pn0", "discover", "--timeout", "-1s")
	if _, errOut, code := n.run("-i", "pn0", "discover", "--format", "xml"); code != 3 || !strings.Contains(errOut, "invalid output format") {
		t.Fatalf("format xml: %d %q", code, errOut)
	}
	// Usage errors render in the requested format.
	msg = n.fails(2, "invalid_input", "nope")
	if !strings.Contains(msg, "unknown command") {
		t.Fatalf("message %q", msg)
	}
	msg = n.fails(2, "invalid_input", "name", "nope")
	if !strings.Contains(msg, `unknown command "nope" for "profinetctl name"`) {
		t.Fatalf("message %q", msg)
	}
	n.fails(2, "invalid_input", "name", "nope", "--station", "x")
	n.fails(2, "invalid_input", "discover", "--bogus")
	if _, errOut, code := n.run("nope"); code != 2 || !strings.HasPrefix(errOut, "error: unknown command") {
		t.Fatalf("text usage error %d %q", code, errOut)
	}
	// A malformed environment variable.
	t.Setenv(EnvTimeout, "soon")
	if _, errOut, code := n.run("version"); code != 3 || !strings.Contains(errOut, EnvTimeout) {
		t.Fatalf("env %d %q", code, errOut)
	}
	t.Setenv(EnvTimeout, "")
	t.Setenv(EnvFormat, "json")
	if out, _, code := n.run("nope"); code != 2 || !strings.Contains(out, `"kind": "usage"`) {
		t.Fatalf("env format %d %q", code, out)
	}
	t.Setenv(EnvInterface, "pn0")
	t.Setenv(EnvFormat, "")
	if out := n.ok("discover", "--wait", "100ms"); !strings.Contains(out, "0 device(s)") {
		t.Fatalf("env interface %q", out)
	}
	// --debug shows the causes.
	n.openErr = errors.New("device busy")
	if _, errOut, _ := n.run("-i", "pn0", "discover", "--debug"); !strings.Contains(errOut, "cause: device busy") {
		t.Fatalf("debug %q", errOut)
	}
}

func TestPcap(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	file := filepath.Join(t.TempDir(), "x.pcap")
	n.ok("-i", "pn0", "--pcap", file, "discover", "--wait", "200ms")
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	r, err := pcap.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	frames := 0
	for {
		if _, _, err := r.Next(); err != nil {
			break
		}
		frames++
	}
	if frames < 2 {
		t.Fatalf("%d frames captured", frames)
	}
	n.fails(3, "output_file_create_failed", "-i", "pn0", "--pcap", filepath.Join(t.TempDir(), "no", "x.pcap"), "discover")
	n.fails(3, "interface_open_failed", "-i", "eth9", "--pcap", filepath.Join(t.TempDir(), "y.pcap"), "discover")
}

func TestVersionAndCompletion(t *testing.T) {
	n := newTestNet(t)
	SetBuildMeta("v1.2.3", "v1.2.3", "abc1234", "2026-10-26")
	defer SetBuildMeta("dev", "", "", "")
	out := n.ok("version")
	for _, want := range []string{"profinetctl version: v1.2.3", "tag:         v1.2.3", "commit:      abc1234", "build date:  2026-10-26", "go version:", "platform:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("version lacks %q:\n%s", want, out)
		}
	}
	for _, sh := range []string{"bash", "zsh", "fish", "powershell"} {
		if out := n.ok("completion", sh); !strings.Contains(out, "profinetctl") {
			t.Fatalf("%s completion", sh)
		}
	}
	n.fails(2, "", "completion", "tcsh")
	// Completion of flags with fixed values.
	out = n.ok("__complete", "reset", "--mode", "")
	if !strings.Contains(out, "factory") || !strings.Contains(out, "communication") {
		t.Fatalf("mode completion %q", out)
	}
	if out := n.ok("__complete", "--format", ""); !strings.Contains(out, "json") {
		t.Fatalf("format completion %q", out)
	}
	if out := n.ok("__complete", "-i", ""); !strings.Contains(out, "pn0") {
		t.Fatalf("interface completion %q", out)
	}
	if out := n.ok("name"); !strings.Contains(out, "Available Commands") {
		t.Fatalf("group help %q", out)
	}
}
