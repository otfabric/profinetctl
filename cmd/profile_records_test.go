// SPDX-License-Identifier: MIT

package cmd

import (
	"slices"
	"strings"
	"testing"

	"github.com/otfabric/go-profinet/device"
	"github.com/otfabric/go-profinet/gsdml"
	"github.com/otfabric/go-profinet/model"
)

// profileMaster is the default of the profile record 0xB600 of
// profileRecordModel: an IO-Link Wireless master's configuration record.
var profileMaster = []byte{0x80, 0x01, 0x01, 0x10, 0xFF, 0xFF, 0xF0, 0xF0, 0xF0, 0xF0, 0xF0, 0xF0, 0xF0, 0xFF, 0x00, 0x05}

// profileRecordModel is mixedAPIModel with parameter records in the profile
// range 0xB000-0xBFFF on its submodule of apiIOLink (slot 4, subslot 2):
// 0xB600 and 0xB900, the indexes IO-Link Wireless master GSDML files use.
func profileRecordModel() *model.Device {
	m := mixedAPIModel()
	sm := &m.Slots[len(m.Slots)-1].Module.Submodules[1]
	sm.Records = []model.Record{
		{Index: 0xB600, Name: "Master configuration", Length: 16, Default: profileMaster},
		{Index: 0xB900, Name: "Port parameters", Length: 4, Default: []byte{0x80, 0x01, 0x00, 0x11}},
	}
	return m
}

func profileGSDMLFile(t *testing.T) string {
	t.Helper()
	doc, err := gsdml.FromModel(profileRecordModel(), gsdml.GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := gsdml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return writeFile(t, "profile.xml", b)
}

// A GSDML file with records in the profile range: valid, no record-index
// warning in gsdml check, its records listed by gsdml submodules and
// planned by gsdml plan, and written by connect at the AR's start.
func TestProfileRecords(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	gf := profileGSDMLFile(t)

	var v validateResult
	n.json(&v, "gsdml", "validate", gf)
	if !v.Valid || len(v.Issues) != 0 {
		t.Fatalf("validate %+v", v)
	}
	var c checkResult
	n.json(&c, "gsdml", "check", gf)
	if c.Errors != 0 || slices.ContainsFunc(c.Issues, func(i gsdml.Issue) bool { return i.Rule == gsdml.RuleRecordIndex || i.Rule == "model" }) {
		t.Fatalf("check %+v", c)
	}

	var s submodulesResult
	n.json(&s, "gsdml", "submodules", gf, "--module", "0x50")
	if len(s.Submodules) != 2 || s.Submodules[1].API != apiIOLink || len(s.Submodules[1].Records) != 2 ||
		s.Submodules[1].Records[0].Index != 0xB600 || s.Submodules[1].Records[0].Default != "800101"+"10ffff"+"f0f0f0f0f0f0f0ff0005" ||
		s.Submodules[1].Records[1].Index != 0xB900 {
		t.Fatalf("submodules %+v", s)
	}

	// gsdml plan: the writes in their submodule's API.
	var p planResult
	n.json(&p, "gsdml", "plan", gf, "--param-defaults")
	if !slices.ContainsFunc(p.Writes, func(w planWriteView) bool {
		return w.API == apiIOLink && w.Slot == 4 && w.Subslot == 2 && w.Index == 0xB600 && w.Data == "80010110fffff0f0f0f0f0f0f0ff0005"
	}) {
		t.Fatalf("plan %+v", p)
	}
	if out := n.ok("gsdml", "plan", gf, "--param-defaults"); !strings.Contains(out, "API 0x00004E01 slot 4 subslot 0x0002 record 46592") {
		t.Fatalf("plan text:\n%s", out)
	}
	if h := strings.Fields(strings.SplitN(n.ok("gsdml", "plan", gf, "--param-defaults", "-o", "table"), "\n", 2)[0]); len(h) == 0 || h[0] != "API" {
		t.Fatalf("plan table header %v", h)
	}
	// A file of API 0 alone: no API in the text or the table.
	plain := paramGSDMLFile(t, paramRecord)
	if out := n.ok("gsdml", "plan", plain, "--param-defaults", "-o", "table"); strings.HasPrefix(out, "API") {
		t.Fatalf("API 0 table:\n%s", out)
	}
	if out := n.ok("gsdml", "plan", plain, "--param-defaults"); strings.Contains(out, "API ") {
		t.Fatalf("API 0 text:\n%s", out)
	}

	d := n.device("dev-a", "192.168.0.10", func(c *device.Config) { c.Model = profileRecordModel() })
	n.ok("-i", "pn0", "connect", "-s", "dev-a", "--gsdml", gf, "--param-defaults", "--duration", "300ms", "--format", "json")
	if w := d.h.recordWrites(); !slices.Contains(w, "4:2:46592=80010110fffff0f0f0f0f0f0f0ff0005") || !slices.Contains(w, "4:2:47360=80010011") {
		t.Fatalf("writes %v", w)
	}
}
