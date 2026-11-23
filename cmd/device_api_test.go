// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-profinet/controller"
	"github.com/otfabric/go-profinet/device"
	"github.com/otfabric/go-profinet/gsdml"
	"github.com/otfabric/go-profinet/model"
	"github.com/otfabric/go-profinet/pnio"
	"github.com/otfabric/go-profinet/rpc"
)

// apiIOLink is the API of the IO-Link submodule of mixedAPIModel.
const apiIOLink uint32 = 0x4E01

// mixedAPIModel is testModel with a module in slot 4 whose subslot 1 is in
// API 0 and subslot 2 in apiIOLink.
func mixedAPIModel() *model.Device {
	m := testModel()
	m.Slots = append(m.Slots, model.Slot{Number: 4, Module: model.Module{Name: "IOL", IdentNumber: 0x50, Submodules: []model.Submodule{
		{Name: "Port", IdentNumber: 0x51, Subslot: 1},
		{Name: "IOL data", IdentNumber: 0x52, API: apiIOLink, Subslot: 2, InputLen: 4},
	}}})
	return m
}

// device info lists the submodules of every API the device has; a device of
// API 0 alone is shown as before.
func TestDeviceInfoAPIs(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10", func(c *device.Config) { c.Model = mixedAPIModel() })
	n.device("dev-b", "192.168.0.11")
	var r deviceInfoResult
	n.json(&r, "-i", "pn0", "device", "info", "-s", "dev-a")
	if r.APIDiscovery != "apidata" || len(r.Errors) != 0 || len(r.Modules) != 6 {
		t.Fatalf("info %+v", r)
	}
	// Slot 4 twice, API order: subslot 1 in API 0, subslot 2 in apiIOLink.
	m4, m5 := r.Modules[4], r.Modules[5]
	if m4.Slot != 4 || m4.API != 0 || len(m4.Submodules) != 1 || m4.Submodules[0].Subslot != 1 ||
		m5.Slot != 4 || m5.API != apiIOLink || m5.IdentNumber != 0x50 || len(m5.Submodules) != 1 || m5.Submodules[0].Subslot != 2 || m5.Submodules[0].IdentNumber != 0x52 {
		t.Fatalf("slot 4 %+v %+v", m4, m5)
	}
	out := n.ok("-i", "pn0", "device", "info", "-s", "dev-a")
	for _, want := range []string{"slot 4   module 0x00000050\n", "slot 4   module 0x00000050 API 0x00004E01\n", "subslot 0x0002 submodule 0x00000052"} {
		if !strings.Contains(out, want) {
			t.Fatalf("text lacks %q:\n%s", want, out)
		}
	}
	table := n.ok("-i", "pn0", "device", "info", "-s", "dev-a", "-o", "table")
	if !strings.HasPrefix(table, "API") || !strings.Contains(table, "0x00004E01") || strings.Count(table, "\n") != 10 {
		t.Fatalf("table:\n%s", table)
	}

	// API 0 alone: no API column, no API in the text.
	var b deviceInfoResult
	n.json(&b, "-i", "pn0", "device", "info", "-s", "dev-b")
	if b.APIDiscovery != "apidata" || len(b.Errors) != 0 || len(b.Modules) != 4 {
		t.Fatalf("API 0 info %+v", b)
	}
	if out := n.ok("-i", "pn0", "device", "info", "-s", "dev-b", "-o", "table"); strings.HasPrefix(out, "API") || strings.Contains(n.ok("-i", "pn0", "device", "info", "-s", "dev-b"), " API ") {
		t.Fatalf("API 0 output names an API:\n%s", out)
	}
}

// fakeIdent answers APIData and RealIdentificationData from tables.
type fakeIdent struct {
	apis    []uint32
	apisErr error
	// ident is the identification each API's read returns; err fails it.
	ident map[uint32]pnio.IdentificationData
	err   map[uint32]error
	raw   map[uint32][]byte
	reads []uint32
}

func (f *fakeIdent) ReadAPIData(context.Context, rpc.UUID) ([]uint32, error) {
	return f.apis, f.apisErr
}

func (f *fakeIdent) Read(_ context.Context, ar rpc.UUID, rec controller.Record, _ uint32) ([]byte, error) {
	if !ar.IsNil() || rec.Index != pnio.IndexRealIdentAPI || rec.Slot != 0 || rec.Subslot != 1 {
		return nil, fmt.Errorf("unexpected read %v %s", ar, rec)
	}
	f.reads = append(f.reads, rec.API)
	if err := f.err[rec.API]; err != nil {
		return nil, err
	}
	if b, ok := f.raw[rec.API]; ok {
		return b, nil
	}
	id := f.ident[rec.API]
	return id.Append(nil), nil
}

func identOf(api uint32, slots ...uint16) pnio.IdentificationData {
	a := pnio.IdentAPI{API: api}
	for _, s := range slots {
		a.Slots = append(a.Slots, pnio.IdentSlot{Slot: s, IdentNumber: uint32(s) << 4, Submodules: []pnio.IdentSubmodule{{Subslot: 1, IdentNumber: 1}}})
	}
	return pnio.IdentificationData{APIs: []pnio.IdentAPI{a}}
}

func noTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, time.Minute)
}

func TestRealIdentDiscovery(t *testing.T) {
	ctx := context.Background()
	refused := &pnio.StatusError{Op: "read", Status: pnio.RWStatus(false, pnio.RWInvalidIndex, 0)}

	// A device without APIData: API 0 is read, and the result says it may
	// be incomplete.
	f := &fakeIdent{apisErr: refused, ident: map[uint32]pnio.IdentificationData{0: identOf(0, 0, 1)}}
	mods, disc, errs := realIdent(ctx, f, noTimeout)
	if disc != discoveryUnavailable || len(mods) != 2 || len(errs) != 1 ||
		!strings.Contains(errs[0], "API discovery unavailable; results may be incomplete") || !strings.HasPrefix(errs[0], "APIData: ") {
		t.Fatalf("unavailable %+v %q %q", mods, disc, errs)
	}

	// Two APIs, listed out of order and twice; one read fails, the other's
	// modules are kept; an entry of another API in an answer is left out.
	other := identOf(apiIOLink, 7)
	other.APIs = append(other.APIs, identOf(0x9999, 8).APIs...)
	f = &fakeIdent{apis: []uint32{apiIOLink, 0, apiIOLink}, ident: map[uint32]pnio.IdentificationData{0: identOf(0, 7, 0), apiIOLink: other}}
	mods, disc, errs = realIdent(ctx, f, noTimeout)
	if disc != discoveryAPIData || len(errs) != 0 || fmt.Sprint(f.reads) != "[0 19969]" || len(mods) != 3 ||
		mods[0].Slot != 0 || mods[1].Slot != 7 || mods[1].API != 0 || mods[2].Slot != 7 || mods[2].API != apiIOLink {
		t.Fatalf("two APIs %+v %q %q %v", mods, disc, errs, f.reads)
	}
	f = &fakeIdent{apis: []uint32{0, apiIOLink}, ident: map[uint32]pnio.IdentificationData{0: identOf(0, 0)},
		err: map[uint32]error{apiIOLink: refused}}
	mods, _, errs = realIdent(ctx, f, noTimeout)
	if len(mods) != 1 || len(errs) != 1 || !strings.HasPrefix(errs[0], "RealIdentificationData (API 0x00004E01): ") {
		t.Fatalf("one API fails %+v %q", mods, errs)
	}
	// API 0 failing keeps the message of before.
	f = &fakeIdent{apis: []uint32{0}, err: map[uint32]error{0: refused}}
	if mods, _, errs = realIdent(ctx, f, noTimeout); len(mods) != 0 || len(errs) != 1 || !strings.HasPrefix(errs[0], "RealIdentificationData: ") {
		t.Fatalf("API 0 fails %+v %q", mods, errs)
	}
	// Data that is no block, and a block that is no identification.
	f = &fakeIdent{apis: []uint32{0, 1}, raw: map[uint32][]byte{0: {1}, 1: (&pnio.APIData{}).Append(nil)}}
	if _, _, errs = realIdent(ctx, f, noTimeout); len(errs) != 2 {
		t.Fatalf("malformed %q", errs)
	}

	// No API listed: API 0 is read, with an error.
	f = &fakeIdent{apis: []uint32{}, ident: map[uint32]pnio.IdentificationData{0: identOf(0, 0)}}
	if mods, disc, errs = realIdent(ctx, f, noTimeout); disc != discoveryAPIData || len(mods) != 1 || len(errs) != 1 || !strings.Contains(errs[0], "no API") {
		t.Fatalf("no API %+v %q %q", mods, disc, errs)
	}

	// More than maxAPIs: the first maxAPIs are read.
	var many []uint32
	for i := range maxAPIs + 5 {
		many = append(many, uint32(i))
	}
	f = &fakeIdent{apis: many}
	if _, _, errs = realIdent(ctx, f, noTimeout); len(f.reads) != maxAPIs || len(errs) != 1 || !strings.Contains(errs[0], "69 APIs") {
		t.Fatalf("bounded: %d reads %q", len(f.reads), errs)
	}
}

// gsdml submodules shows the API of a submodule of an API other than 0, read
// from the VirtualSubmoduleItem's API attribute; a file of API 0 alone is
// listed as before.
func TestGSDMLSubmodulesAPI(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	doc, err := gsdml.FromModel(mixedAPIModel(), gsdml.GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := gsdml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	file := writeFile(t, "mixed.xml", b)
	var r submodulesResult
	n.json(&r, "gsdml", "submodules", file, "--module", "0x50")
	if len(r.Submodules) != 2 || r.Submodules[0].API != 0 || r.Submodules[0].Subslot != 1 ||
		r.Submodules[1].API != apiIOLink || r.Submodules[1].Subslot != 2 || r.Submodules[1].IdentNumber != 0x52 {
		t.Fatalf("submodules %+v", r)
	}
	out := n.ok("gsdml", "submodules", file, "--module", "0x50")
	if !strings.Contains(out, "0x00000050/0x00000051  subslot 0x0001") || !strings.Contains(out, "0x00000050/0x00000052  API 0x00004E01  subslot 0x0002") {
		t.Fatalf("text:\n%s", out)
	}
	table := n.ok("gsdml", "submodules", file, "--module", "0x50", "-o", "table")
	if h := strings.Fields(strings.SplitN(table, "\n", 2)[0]); len(h) != 8 || h[2] != "API" || !strings.Contains(table, "0x00004E01") || !strings.Contains(table, "0x00000000") {
		t.Fatalf("table:\n%s", table)
	}

	// API 0 alone: no API column, no API in the text.
	plain := gsdmlFile(t)
	if out := n.ok("gsdml", "submodules", plain, "-o", "table"); strings.Contains(out, "API") {
		t.Fatalf("API 0 table:\n%s", out)
	}
	if out := n.ok("gsdml", "submodules", plain); strings.Contains(out, "API") {
		t.Fatalf("API 0 text:\n%s", out)
	}
}
