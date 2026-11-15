// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"
	"math"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-profinet/dcp"
	"github.com/otfabric/go-profinet/ethernet"
	"github.com/otfabric/go-profinet/l2"
	"github.com/otfabric/go-profinet/vnet"
)

// fakeStations is one endpoint that answers every DCP Identify, whatever its
// filter, with the identities it was given, each from the identity's MAC
// address. It answers nothing else and counts the other DCP requests it
// receives (Get and Set).
type fakeStations struct {
	requests atomic.Int64
}

// fakeStations attaches the endpoint; its own address is the first
// identity's MAC.
func (n *testNet) fakeStations(ids ...dcp.Identity) *fakeStations {
	t := n.t
	t.Helper()
	ep, err := n.net.NewEndpoint(vnet.EndpointConfig{MAC: ids[0].MAC})
	if err != nil {
		t.Fatal(err)
	}
	if err := ep.JoinMulticast(ethernet.DCPIdentifyMulticast); err != nil {
		t.Fatal(err)
	}
	mux := l2.NewMux(ep)
	sub, err := mux.Subscribe(l2.MatchFrameIDs(dcp.FrameIDGetSet, dcp.FrameIDIdentifyRequest), 64)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	fs := &fakeStations{}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		sub.Close()
		wg.Wait()
		_ = mux.Close()
		_ = ep.Close()
	})
	wg.Go(func() {
		for {
			fr, err := sub.Recv(ctx)
			if err != nil {
				return
			}
			req, err := dcp.Decode(fr.Payload)
			if err != nil {
				continue
			}
			if req.FrameID != dcp.FrameIDIdentifyRequest {
				fs.requests.Add(1)
				continue
			}
			for _, id := range ids {
				resp := &dcp.Frame{FrameID: dcp.FrameIDIdentifyResponse, Service: dcp.ServiceIdentify, Type: dcp.TypeResponseSuccess, Xid: req.Xid, Blocks: id.Blocks()}
				eh := ethernet.Frame{Dst: fr.Src, Src: id.MAC, EtherType: ethernet.EtherTypeProfinet}
				if b, err := resp.AppendTo(eh.AppendHeader(nil)); err == nil {
					_ = ep.WriteFrame(ctx, ethernet.Pad(b, 0))
				}
			}
		}
	})
	return fs
}

// stationIdentity is the identity of a station of the test vendor with
// address ip (/24).
func stationIdentity(mac ethernet.MAC, name, ip string) dcp.Identity {
	return dcp.Identity{MAC: mac, NameOfStation: name, VendorID: 0xF000, DeviceID: 0x0101, Role: dcp.RoleIODevice, IP: suite(ip)}
}

// A station that answers an Identify for another name is not taken for the
// station named: the commands find no device (exit code 11) and send it
// nothing.
func TestStationAnswersWithAnotherName(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	fs := n.fakeStations(stationIdentity(ethernet.MAC{0x02, 0xAA, 0, 0, 0, 1}, "other-device", "192.168.0.50"))
	for _, args := range [][]string{
		{"identify", "--station", "pump-7"},
		{"signal", "--station", "pump-7"},
		{"name", "set", "new-name", "--station", "pump-7"},
		{"ip", "set", "192.168.0.77/24", "--station", "pump-7"},
		{"record", "write", "--station", "pump-7", "--index", "1", "--data", "00"},
	} {
		msg := n.fails(11, "device_not_found", append(append([]string{"-i", "pn0"}, args...), "--wait", "300ms")...)
		if !strings.Contains(msg, "1 answer(s) with another station name ignored") {
			t.Fatalf("%s: %s", strings.Join(args, " "), msg)
		}
	}
	if got := fs.requests.Load(); got != 0 {
		t.Fatalf("the station named other-device received %d DCP Get or Set requests", got)
	}
	var dr discoverResult
	n.json(&dr, "-i", "pn0", "discover", "--station", "pump-7", "--wait", "300ms")
	if len(dr.Devices) != 0 {
		t.Fatalf("discover --station pump-7: %+v", dr.Devices)
	}
	// With the station named on the segment as well, it is the one found.
	n.device("pump-7", "192.168.0.30")
	var ir identifyResult
	n.json(&ir, "-i", "pn0", "identify", "--station", "pump-7", "--wait", "300ms")
	if ir.Device.NameOfStation != "pump-7" || ir.Device.IP != "192.168.0.30" {
		t.Fatalf("identify --station pump-7: %+v", ir.Device)
	}
	n.json(&dr, "-i", "pn0", "discover", "--station", "pump-7", "--wait", "300ms")
	if len(dr.Devices) != 1 || dr.Devices[0].NameOfStation != "pump-7" {
		t.Fatalf("discover --station pump-7: %+v", dr.Devices)
	}
	if got := fs.requests.Load(); got != 0 {
		t.Fatalf("the station named other-device received %d DCP Get or Set requests", got)
	}
}

func TestStationsNamed(t *testing.T) {
	ids := []dcp.Identity{{NameOfStation: "a"}, {NameOfStation: "b"}, {NameOfStation: ""}, {NameOfStation: "a"}, {NameOfStation: "a.b"}}
	for _, c := range []struct {
		name string
		want int
	}{{"a", 2}, {"b", 1}, {"a.b", 1}, {"A", 0}, {"c", 0}} {
		if got := stationsNamed(ids, c.name); len(got) != c.want {
			t.Errorf("stationsNamed(%q): %d, want %d", c.name, len(got), c.want)
		}
	}
	if got := stationsNamed(nil, "a"); len(got) != 0 {
		t.Errorf("stationsNamed(nil): %v", got)
	}
}

// topology reads only the stations in a subnet of the interface (pn0 is
// 192.168.0.2/24); --read-any-address reads the others too.
func TestTopologyReadsTheSubnetsOfTheInterface(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	n.fakeStations(stationIdentity(ethernet.MAC{0x02, 0xAA, 0, 0, 0, 1}, "far", "10.99.0.1"))
	nodes := func(args ...string) map[string]topoNode {
		t.Helper()
		out, errOut, code := n.run(append([]string{"-i", "pn0", "topology", "--wait", "300ms", "--timeout", "500ms", "--format", "json"}, args...)...)
		if code != 7 {
			t.Fatalf("topology %v: exit %d, want 7\n%s%s", args, code, out, errOut)
		}
		var tr topologyResult
		decode(t, out, &tr)
		byName := map[string]topoNode{}
		for _, nd := range tr.Nodes {
			byName[nd.Station] = nd
		}
		if a := byName["dev-a"]; a.Error != "" || len(a.Ports) != 1 {
			t.Fatalf("topology %v: dev-a %+v", args, a)
		}
		return byName
	}
	if far := nodes()["far"]; far.Error != "IP address outside the subnets of pn0: not read (--read-any-address reads it)" || far.IP != "10.99.0.1" {
		t.Fatalf("far %+v", far)
	}
	if far := nodes("--read-any-address")["far"]; !strings.HasPrefix(far.Error, "PDRealData: ") {
		t.Fatalf("far with --read-any-address %+v", far)
	}
}

func TestInSubnets(t *testing.T) {
	subnets := []netip.Prefix{netip.MustParsePrefix("192.168.0.0/24"), netip.MustParsePrefix("10.0.0.0/30")}
	for _, c := range []struct {
		addr string
		want bool
	}{
		{"192.168.0.1", true}, {"192.168.0.0", true}, {"192.168.0.255", true}, {"192.168.1.0", false}, {"192.167.255.255", false},
		{"10.0.0.3", true}, {"10.0.0.4", false}, {"0.0.0.0", false}, {"::ffff:192.168.0.1", false}, {"fe80::1", false},
	} {
		if got := inSubnets(netip.MustParseAddr(c.addr), subnets); got != c.want {
			t.Errorf("inSubnets(%s): %v, want %v", c.addr, got, c.want)
		}
	}
	if inSubnets(netip.MustParseAddr("192.168.0.1"), nil) {
		t.Error("an interface without addresses has no subnet")
	}
	if inSubnets(netip.Addr{}, subnets) {
		t.Error("the zero address is in no subnet")
	}
}

func TestSubnets(t *testing.T) {
	n := newTestNet(t)
	for _, c := range []struct {
		iface string
		want  string
	}{{"pn0", "[192.168.0.0/24]"}, {"pn1", "[]"}, {"nope", "[]"}} {
		a := &app{backend: n, iface: c.iface}
		if got := fmt.Sprint(a.subnets()); got != c.want {
			t.Errorf("subnets of %s: %s, want %s", c.iface, got, c.want)
		}
	}
}

func TestReadPhase(t *testing.T) {
	for _, c := range []struct{ timeout, want time.Duration }{
		{1, 2},
		{5 * time.Second, 10 * time.Second},
		{math.MaxInt64/2 - 1, math.MaxInt64 - 3},
		{math.MaxInt64 / 2, math.MaxInt64 - 1},
		{math.MaxInt64/2 + 1, math.MaxInt64 - 1},
		{math.MaxInt64, math.MaxInt64 - 1},
	} {
		if got := readPhase(c.timeout); got != c.want || got <= 0 {
			t.Errorf("readPhase(%d): %d, want %d", c.timeout, got, c.want)
		}
	}
}

// The reads of topology end after 2 × --timeout, however many stations
// answer: 32 stations whose address nobody answers, 8 read at a time, each
// read holding its slot for at least --timeout, leave at least 8 not read.
func TestTopologyReadsEndAfterTwoTimeouts(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	var ids []dcp.Identity
	for i := range 32 {
		ids = append(ids, stationIdentity(ethernet.MAC{0x02, 0xAA, 0, 0, 1, byte(i)}, fmt.Sprintf("st-%02d", i), fmt.Sprintf("192.168.0.%d", 100+i)))
	}
	n.fakeStations(ids...)
	out, errOut, code := n.run("-i", "pn0", "topology", "--wait", "300ms", "--timeout", "300ms", "--format", "json")
	if code != 7 {
		t.Fatalf("exit %d, want 7\n%s%s", code, out, errOut)
	}
	var tr topologyResult
	decode(t, out, &tr)
	notRead := 0
	for _, nd := range tr.Nodes {
		switch nd.Error {
		case "not read: the reads ended after 600ms (2 × --timeout)":
			notRead++
		case "":
			t.Fatalf("%s was read: %+v", nd.Station, nd)
		}
	}
	if len(tr.Nodes) != len(ids) || notRead < len(ids)-3*topologyReads {
		t.Fatalf("%d stations, %d not read:\n%s", len(tr.Nodes), notRead, out)
	}
}
