// SPDX-License-Identifier: MIT

package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/go-profinet/device"
	"github.com/otfabric/go-profinet/ethernet"
	"github.com/otfabric/go-profinet/l2"
	"github.com/otfabric/go-profinet/l2/bridge"
	"github.com/otfabric/go-profinet/model"
	"github.com/otfabric/go-profinet/mrp"
	"github.com/otfabric/go-profinet/netstack"
	"github.com/otfabric/go-profinet/vnet"
)

// gsdxFile builds a small OPC container holding a GSDML file.
func gsdxFile(t *testing.T, n *testNet, extra bool) string {
	t.Helper()
	xmlFile := filepath.Join(t.TempDir(), "dev.xml")
	n.ok("gsdml", "generate", "--model", modelFile(t), "-O", xmlFile)
	gx, err := os.ReadFile(xmlFile)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, b []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(b)
	}
	add("[Content_Types].xml", []byte(`<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`+
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`+
		`<Default Extension="xml" ContentType="application/xml"/></Types>`))
	add("_rels/.rels", []byte(`<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="r1" Type="http://example.org/gsdml" Target="GSDML-V2.35-OTF-TEST-20261027.xml"/></Relationships>`))
	add("GSDML-V2.35-OTF-TEST-20261027.xml", gx)
	if extra {
		add("unreached.bin", []byte{1, 2, 3})
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return writeFile(t, "dev.gsdx", buf.Bytes())
}

func TestGSDX(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	f := gsdxFile(t, n, false)
	var r struct {
		Package struct {
			Parts  []struct{ Name, ContentType string }
			Issues []struct{ Part, Message string }
		}
		GSDML []gsdxPart
		Scope string
	}
	n.json(&r, "gsdml", "gsdx", f)
	if len(r.Package.Parts) != 2 || len(r.GSDML) != 1 || r.GSDML[0].Errors != 0 || !strings.Contains(r.Scope, "not verified") {
		t.Fatalf("report %+v", r)
	}
	out := n.ok("gsdml", "gsdx", f)
	if !strings.Contains(out, "2 part(s)") || !strings.Contains(out, "GSDML /GSDML-V2.35-OTF-TEST-20261027.xml: 0 error(s)") {
		t.Fatalf("text:\n%s", out)
	}
	// Extract the GSDML part.
	x := n.ok("gsdml", "gsdx", f, "--extract", "/GSDML-V2.35-OTF-TEST-20261027.xml")
	if !strings.HasPrefix(x, "<?xml") {
		t.Fatalf("extract %q", x[:20])
	}
	dst := filepath.Join(t.TempDir(), "out.xml")
	n.ok("gsdml", "gsdx", f, "--extract", "/GSDML-V2.35-OTF-TEST-20261027.xml", "-O", dst)
	if b, err := os.ReadFile(dst); err != nil || !bytes.HasPrefix(b, []byte("<?xml")) {
		t.Fatalf("extract to file: %v", err)
	}
	n.fails(3, "invalid_input", "gsdml", "gsdx", f, "--extract", "/nothing")
	n.fails(3, "invalid_input", "gsdml", "gsdx", writeFile(t, "bad.gsdx", []byte("not a zip")))
	// A part no relationship reaches is fine; a bad container is not.
	if _, _, code := n.run("gsdml", "gsdx", gsdxFile(t, n, true)); code != 0 && code != 7 {
		t.Fatalf("exit %d", code)
	}
}

// mrpDevice starts a two-port device with MRP: port 1 on the test network,
// port 2 on a segment of its own.
func (n *testNet) mrpDevice(name, ip string, role mrp.Role) {
	n.t.Helper()
	n.mu.Lock()
	n.next += 3
	base := n.next
	n.mu.Unlock()
	p1, err := n.net.NewEndpoint(vnet.EndpointConfig{MAC: ethernet.MAC{0x02, 0, 0, 0, 1, base}, Promiscuous: true})
	if err != nil {
		n.t.Fatal(err)
	}
	other := vnet.New(9)
	p2, err := other.NewEndpoint(vnet.EndpointConfig{MAC: ethernet.MAC{0x02, 0, 0, 0, 1, base + 1}, Promiscuous: true})
	if err != nil {
		n.t.Fatal(err)
	}
	br, err := bridge.New(bridge.Config{Ports: []l2.Port{p1, p2}, MAC: ethernet.MAC{0x02, 0, 0, 0, 1, base + 2}})
	if err != nil {
		n.t.Fatal(err)
	}
	mux := l2.NewMux(br.CPU())
	s := suite(ip)
	st, err := netstack.New(mux, netstack.Config{Addr: s.Addr, Mask: s.Mask})
	if err != nil {
		n.t.Fatal(err)
	}
	m := testModel()
	m.Capabilities.Ports = 2
	m.DAP = model.StandardDAP(m.DAP.Name, m.DAP.IdentNumber, 2)
	d, err := device.New(device.Config{Model: m, Mux: mux, Bridge: br, StationName: name, IP: s, ListenPacket: st.ListenPacket,
		NoResponseDelay: true, MRP: &device.MRPConfig{Role: role}})
	if err != nil {
		n.t.Fatal(err)
	}
	n.t.Cleanup(func() {
		_ = d.Close()
		_ = st.Close()
		_ = mux.Close()
		_ = br.Close()
		_ = p1.Close()
		_ = p2.Close()
		_ = other.Close()
	})
}

func TestMRPStatus(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.mrpDevice("ring-a", "192.168.0.40", mrp.RoleManager)
	n.device("plain", "192.168.0.41")
	var r mrpResult
	n.json(&r, "-i", "pn0", "mrp", "status", "-s", "ring-a")
	if r.Role != "manager" || r.DomainName != "mrpdomain-1" || r.Domain != "ffffffff-ffff-ffff-ffff-ffffffffffff" || r.RingState != "open" || len(r.RingPorts) != 2 {
		t.Fatalf("status %+v", r)
	}
	if out := n.ok("-i", "pn0", "mrp", "status", "-s", "ring-a"); !strings.Contains(out, "MRP manager") || !strings.Contains(out, "ring open") {
		t.Fatalf("text:\n%s", out)
	}
	if _, _, code := n.run("-i", "pn0", "mrp", "status", "-s", "plain"); code == 0 {
		t.Fatal("a device without MRP")
	}
	// The role, written by an AR.
	n.ok("-i", "pn0", "connect", "-s", "ring-a", "--model", writeFile(t, "two.json", twoPortModel(t)), "--mrp-role", "client", "--duration", "300ms")
	r = mrpResult{}
	n.json(&r, "-i", "pn0", "mrp", "status", "-s", "ring-a")
	if r.Role != "client" || r.RingState != "" {
		t.Fatalf("after --mrp-role client: %+v", r)
	}
	n.fails(3, "invalid_input", "-i", "pn0", "connect", "-s", "ring-a", "--model", modelFile(t), "--mrp-role", "boss", "--duration", "100ms")
}

func twoPortModel(t *testing.T) []byte {
	t.Helper()
	m := testModel()
	m.Capabilities.Ports = 2
	m.DAP = model.StandardDAP(m.DAP.Name, m.DAP.IdentNumber, 2)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
