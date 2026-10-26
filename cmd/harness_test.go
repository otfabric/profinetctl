// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-profinet"
	"github.com/otfabric/go-profinet/dcp"
	"github.com/otfabric/go-profinet/device"
	"github.com/otfabric/go-profinet/ethernet"
	"github.com/otfabric/go-profinet/gsdml"
	"github.com/otfabric/go-profinet/l2"
	"github.com/otfabric/go-profinet/l2/osport"
	"github.com/otfabric/go-profinet/model"
	"github.com/otfabric/go-profinet/netstack"
	"github.com/otfabric/go-profinet/pnio"
	"github.com/otfabric/go-profinet/vnet"
)

func TestMain(m *testing.M) {
	// Cycles of 4 ms with a hold of 3 cycles are too short for the race
	// detector on a loaded machine.
	defaultWatchdog, defaultDataHold = 50, 50
	os.Exit(m.Run())
}

// testModel is the device of the tests: 8 octets in (slot 1), 4 octets out
// with a parameter record (slot 2), 2 in and 2 out (slot 3).
func testModel() *model.Device {
	return &model.Device{
		Identity: model.Identity{VendorID: 0xF000, DeviceID: 0x0101, VendorName: "OT Fabric", TypeOfStation: "profinetctl test device",
			Family: "Tests", OrderID: "OTF-CTL-TEST", SerialNumber: "0042", HardwareRevision: 3,
			SoftwareRevision: pnio.SoftwareRevision{Prefix: 'V', FunctionalEnhance: 1, BugFix: 2}},
		Capabilities: model.Capabilities{MaxARs: 2, MinDeviceInterval: 32, ConformanceClass: "B", Ports: 1, LLDP: true},
		DAP:          model.StandardDAP("profinetctl test DAP", 0x00000100, 1),
		Slots: []model.Slot{
			{Number: 1, Module: model.Module{Name: "8 octets in", IdentNumber: 0x10, Submodules: []model.Submodule{{Name: "Inputs", IdentNumber: 0x11, Subslot: 1, InputLen: 8}}}},
			{Number: 2, Module: model.Module{Name: "4 octets out", IdentNumber: 0x20, Submodules: []model.Submodule{{Name: "Outputs", IdentNumber: 0x21, Subslot: 1, OutputLen: 4,
				Records: []model.Record{{Index: 0x0010, Name: "Mode", Length: 2, Default: []byte{0, 1}}}}}}},
			{Number: 3, Module: model.Module{Name: "2 in 2 out", IdentNumber: 0x30, Submodules: []model.Submodule{{Name: "IO", IdentNumber: 0x31, Subslot: 1, InputLen: 2, OutputLen: 2}}}},
		},
		Catalog: []model.Module{
			{Name: "16 octets in", IdentNumber: 0x40, Submodules: []model.Submodule{{Name: "Wide inputs", IdentNumber: 0x41, Subslot: 1, InputLen: 16}}},
		},
	}
}

// recorder is the handler of a test device: it records the DCP requests.
type recorder struct {
	device.NopHandler
	mu      sync.Mutex
	signals int
	resets  []uint16
}

func (r *recorder) Signal() {
	r.mu.Lock()
	r.signals++
	r.mu.Unlock()
}

func (r *recorder) ResetToFactory(mode uint16) dcp.BlockError {
	r.mu.Lock()
	r.resets = append(r.resets, mode)
	r.mu.Unlock()
	return dcp.BlockErrorNone
}

// testDev is a go-profinet IO-Device on the virtual network.
type testDev struct {
	dev  *device.Device
	mac  ethernet.MAC
	h    *recorder
	addr string
}

// testNet is a virtual network with devices, and the backend profinetctl
// runs on.
type testNet struct {
	t      *testing.T
	net    *vnet.Network
	devs   map[string]*testDev
	next   byte
	opened []string
	// openErr makes Open fail.
	openErr error
	mu      sync.Mutex
}

func newTestNet(t *testing.T) *testNet {
	t.Helper()
	n := &testNet{t: t, net: vnet.New(1), devs: map[string]*testDev{}, next: 0x10}
	t.Cleanup(func() { _ = n.net.Close() })
	return n
}

func suite(ip string) dcp.IPSuite {
	if ip == "" {
		return dcp.IPSuite{}
	}
	return dcp.IPSuite{Addr: netip.MustParseAddr(ip), Mask: netip.MustParseAddr("255.255.255.0"), Gateway: netip.IPv4Unspecified()}
}

// endpoint attaches an endpoint with an IP stack.
func (n *testNet) endpoint(ip string) (*vnet.Endpoint, *l2.Mux, *netstack.Stack, ethernet.MAC) {
	n.t.Helper()
	n.mu.Lock()
	n.next++
	mac := ethernet.MAC{0x02, 0, 0, 0, 0, n.next}
	n.mu.Unlock()
	ep, err := n.net.NewEndpoint(vnet.EndpointConfig{MAC: mac})
	if err != nil {
		n.t.Fatal(err)
	}
	mux := l2.NewMux(ep)
	s := suite(ip)
	st, err := netstack.New(mux, netstack.Config{Addr: s.Addr, Mask: s.Mask})
	if err != nil {
		n.t.Fatal(err)
	}
	n.t.Cleanup(func() {
		_ = st.Close()
		_ = mux.Close()
		_ = ep.Close()
	})
	return ep, mux, st, mac
}

// device starts an IO-Device.
func (n *testNet) device(name, ip string, opts ...func(*device.Config)) *testDev {
	n.t.Helper()
	_, mux, st, mac := n.endpoint(ip)
	h := &recorder{}
	cfg := device.Config{
		Model: testModel(), Mux: mux, StationName: name, IP: suite(ip), ListenPacket: st.ListenPacket,
		ApplyIP: func(s dcp.IPSuite, _ bool) error { return st.SetAddr(s.Addr, s.Mask, s.Gateway) },
		Handler: h, NoResponseDelay: true, LLDPInterval: 100 * time.Millisecond,
		IM:     device.IMData{IM1: pnio.IM1{TagFunction: "pump", TagLocation: "hall 1"}},
		Logger: profinet.NopLogger(),
	}
	for _, o := range opts {
		o(&cfg)
	}
	d, err := device.New(cfg)
	if err != nil {
		n.t.Fatal(err)
	}
	n.t.Cleanup(func() { _ = d.Close() })
	td := &testDev{dev: d, mac: mac, h: h, addr: ip}
	n.devs[name] = td
	return td
}

func (n *testNet) Name() string { return "vnet" }

func (n *testNet) Interfaces() ([]osport.Interface, error) {
	return []osport.Interface{{Name: "pn0", MAC: "02:00:00:00:00:01", MTU: 1500, Up: true, Addrs: []string{"192.168.0.2/24"}}, {Name: "pn1", MAC: "02:00:00:00:00:02", MTU: 1500}}, nil
}

// Open attaches the host of profinetctl, 192.168.0.2, to the network.
func (n *testNet) Open(iface string, wrap func(l2.Port) l2.Port) (*Link, error) {
	if n.openErr != nil {
		return nil, n.openErr
	}
	if iface != "pn0" {
		return nil, fmt.Errorf("interface %q: %w", iface, profinet.ErrNotFound)
	}
	n.mu.Lock()
	n.next++
	mac := ethernet.MAC{0x02, 0, 0, 0, 1, n.next}
	n.opened = append(n.opened, iface)
	n.mu.Unlock()
	ep, err := n.net.NewEndpoint(vnet.EndpointConfig{MAC: mac})
	if err != nil {
		return nil, err
	}
	var port l2.Port = ep
	if wrap != nil {
		port = wrap(ep)
	}
	mux := l2.NewMux(port)
	st, err := netstack.New(mux, netstack.Config{Addr: netip.MustParseAddr("192.168.0.2"), Mask: netip.MustParseAddr("255.255.255.0")})
	if err != nil {
		return nil, err
	}
	return &Link{Mux: mux, ListenPacket: st.ListenPacket, close: func() {
		_ = st.Close()
		_ = mux.Close()
		_ = ep.Close()
	}}, nil
}

// run runs profinetctl on the network and returns its output and exit code.
func (n *testNet) run(args ...string) (string, string, int) {
	n.t.Helper()
	var out, errb bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	code := Run(ctx, n, args, &out, &errb)
	return out.String(), errb.String(), code
}

// ok runs profinetctl and fails unless it exits with code 0.
func (n *testNet) ok(args ...string) string {
	n.t.Helper()
	out, errOut, code := n.run(args...)
	if code != 0 {
		n.t.Fatalf("profinetctl %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, out, errOut)
	}
	return out
}

// json runs profinetctl with --format json and decodes its output.
func (n *testNet) json(v any, args ...string) {
	n.t.Helper()
	out := n.ok(append(args, "--format", "json")...)
	if err := json.Unmarshal([]byte(out), v); err != nil {
		n.t.Fatalf("profinetctl %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// fails runs profinetctl and checks its exit code and the error code of its JSON
// error.
func (n *testNet) fails(code int, errCode string, args ...string) string {
	n.t.Helper()
	out, errOut, got := n.run(append(args, "--format", "json")...)
	if got != code {
		n.t.Fatalf("profinetctl %s: exit %d, want %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), got, code, out, errOut)
	}
	var env struct {
		Error struct{ Kind, Code, Message string }
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		n.t.Fatalf("profinetctl %s: no JSON error: %v\n%s", strings.Join(args, " "), err, out)
	}
	if errCode != "" && env.Error.Code != errCode {
		n.t.Fatalf("profinetctl %s: error code %q, want %q: %s", strings.Join(args, " "), env.Error.Code, errCode, env.Error.Message)
	}
	return env.Error.Message
}

// jsonLines decodes JSON Lines output.
func jsonLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var evs []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("line %q: %v", l, err)
		}
		evs = append(evs, m)
	}
	return evs
}

// writeFile writes a file in the test's temporary directory.
func writeFile(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// modelFile writes the test model as JSON.
func modelFile(t *testing.T) string {
	t.Helper()
	b, err := json.Marshal(testModel())
	if err != nil {
		t.Fatal(err)
	}
	return writeFile(t, "model.json", b)
}

// gsdmlFile writes the GSDML file of the test model.
func gsdmlFile(t *testing.T) string {
	t.Helper()
	doc, err := gsdml.FromModel(testModel(), gsdml.GenerateOptions{SupervisorAR: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := gsdml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return writeFile(t, "device.xml", b)
}

func decode(t *testing.T, s string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("%v:\n%s", err, s)
	}
}
