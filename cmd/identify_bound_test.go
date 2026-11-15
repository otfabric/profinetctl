// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/otfabric/go-profinet/dcp"
	"github.com/otfabric/go-profinet/ethernet"
	"github.com/otfabric/go-profinet/l2"
	"github.com/otfabric/go-profinet/vnet"
)

// identifyKept is the number of stations one DCP Identify keeps (go-profinet).
const identifyKept = 4096

// floodMax bounds the answers identifyFlood sends to one Identify.
const floodMax = 1 << 15

// identifyFlood counts the frames the other endpoints of the network send.
type identifyFlood struct {
	// identifies counts their DCP Identify requests, others every other frame.
	identifies, others atomic.Int64
}

// identifyFlood attaches an endpoint that answers every DCP Identify, whatever
// its filter, with answers from distinct MAC addresses, one station each
// (st-0, st-1, ..., without an IP address): more stations than an Identify
// keeps. It answers until the endpoint that asked has left the network (at
// most floodMax answers), so that an answer a full queue drops on the way only
// makes the next one count.
func (n *testNet) identifyFlood() *identifyFlood {
	t := n.t
	t.Helper()
	ep, err := n.net.NewEndpoint(vnet.EndpointConfig{MAC: ethernet.MAC{0x02, 0xBB, 0, 0, 0, 0}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ep.JoinMulticast(ethernet.DCPIdentifyMulticast); err != nil {
		t.Fatal(err)
	}
	fl := &identifyFlood{}
	n.net.AddTap(func(from *vnet.Endpoint, frame []byte) {
		if from == ep {
			return
		}
		if et, off, ok := ethernet.PeekEtherType(frame); ok && et == ethernet.EtherTypeProfinet && len(frame) >= off+2 &&
			binary.BigEndian.Uint16(frame[off:]) == dcp.FrameIDIdentifyRequest {
			fl.identifies.Add(1)
			return
		}
		fl.others.Add(1)
	})
	mux := l2.NewMux(ep)
	sub, err := mux.Subscribe(l2.MatchFrameIDs(dcp.FrameIDIdentifyRequest, dcp.FrameIDIdentifyRequest), 64)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		sub.Close()
		wg.Wait()
		_ = mux.Close()
		_ = ep.Close()
	})
	attached := func(mac ethernet.MAC) bool {
		for _, e := range n.net.Endpoints() {
			if e.HardwareAddr() == mac {
				return true
			}
		}
		return false
	}
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
			for i := 0; i < floodMax && ctx.Err() == nil; i++ {
				if i%64 == 63 {
					if !attached(fr.Src) {
						break
					}
					runtime.Gosched()
				}
				mac := ethernet.MAC{0x02, 0xBB}
				binary.BigEndian.PutUint32(mac[2:], uint32(i+1))
				id := dcp.Identity{MAC: mac, NameOfStation: fmt.Sprintf("st-%d", i), VendorID: 0xF000, DeviceID: 0x0101, Role: dcp.RoleIODevice}
				resp := &dcp.Frame{FrameID: dcp.FrameIDIdentifyResponse, Service: dcp.ServiceIdentify, Type: dcp.TypeResponseSuccess, Xid: req.Xid, Blocks: id.Blocks()}
				eh := ethernet.Frame{Dst: fr.Src, Src: mac, EtherType: ethernet.EtherTypeProfinet}
				if b, err := resp.AppendTo(eh.AppendHeader(nil)); err == nil {
					_ = ep.WriteFrame(ctx, ethernet.Pad(b, 0))
				}
			}
		}
	})
	return fl
}

// When more stations answer than an Identify keeps, discover and topology
// show those of the answers kept, marked truncated, with exit code 7.
func TestTruncatedIdentify(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.identifyFlood()
	line := fmt.Sprintf("truncated: more than %d devices answered DCP Identify; those after the first %d answers are not listed\n", identifyKept, identifyKept)
	// The result as docs/json-output.md describes it: the devices of
	// discover have name_of_station, those of topology station.
	type truncatedJSON struct {
		Devices []struct {
			NameOfStation string `json:"name_of_station"`
		} `json:"devices"`
		Truncated bool `json:"truncated"`
	}
	for _, cmd := range []string{"discover", "topology"} {
		out, errOut, code := n.run("-i", "pn0", cmd, "--wait", "30s", "--format", "json")
		var r truncatedJSON
		if err := json.Unmarshal([]byte(out), &r); code != 7 || err != nil || len(r.Devices) != identifyKept || !r.Truncated {
			t.Fatalf("%s: exit %d, %d devices, truncated %v (%v)\nstderr: %s\nstdout: %.300s", cmd, code, len(r.Devices), r.Truncated, err, errOut, out)
		}
		out, errOut, code = n.run("-i", "pn0", cmd, "--wait", "30s")
		if code != 7 || !strings.HasSuffix(out, line) {
			t.Fatalf("%s, text: exit %d\nstderr: %s\nstdout ends: %q", cmd, code, errOut, out[max(0, len(out)-200):])
		}
	}
	// With --station, the stations kept that carry the name.
	out, errOut, code := n.run("-i", "pn0", "discover", "--station", "st-1", "--wait", "30s", "--format", "json")
	var r truncatedJSON
	if err := json.Unmarshal([]byte(out), &r); code != 7 || err != nil || !r.Truncated || len(r.Devices) > 1 || (len(r.Devices) == 1 && r.Devices[0].NameOfStation != "st-1") {
		t.Fatalf("discover --station st-1: exit %d (%v)\nstderr: %s\nstdout: %.300s", code, err, errOut, out)
	}
}

// When more stations answer the Identify of a lookup by name than it keeps,
// the station cannot be told apart: the command exits 6
// (duplicate_station_name) and sends nothing but the Identify.
func TestStationLookupWithTruncatedIdentify(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	fl := n.identifyFlood()
	cmds := [][]string{
		{"identify", "--station", "st-1"},
		{"signal", "--station", "st-1"},
		{"name", "set", "new-name", "--station", "st-1"},
		{"ip", "set", "192.168.0.77/24", "--station", "st-1"},
		{"record", "write", "--station", "st-1", "--index", "1", "--data", "00"},
		{"im", "write", "--station", "st-1", "--function", "valve"},
	}
	for _, args := range cmds {
		msg := n.fails(6, "duplicate_station_name", append(append([]string{"-i", "pn0"}, args...), "--wait", "30s")...)
		if want := fmt.Sprintf("more than %d devices answer the DCP Identify for station \"st-1\"", identifyKept); !strings.HasPrefix(msg, want) {
			t.Fatalf("%s: %s", strings.Join(args, " "), msg)
		}
	}
	if got := fl.identifies.Load(); got != int64(len(cmds)) {
		t.Fatalf("%d DCP Identify requests, want %d", got, len(cmds))
	}
	if got := fl.others.Load(); got != 0 {
		t.Fatalf("profinetctl sent %d frames besides its DCP Identify requests", got)
	}
}
