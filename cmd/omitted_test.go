// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"

	"github.com/otfabric/go-profinet/gsdml"
)

// omittedGSDMLFile writes the test model's GSDML file with a configurable
// submodule (UseableSubmodules, UsedInSubslots) on the catalog module
// 0x40, which no slot plugs: gsdml.ToModel leaves it out of the catalog.
func omittedGSDMLFile(t *testing.T) string {
	t.Helper()
	doc, err := gsdml.FromModel(testModel(), gsdml.GenerateOptions{SupervisorAR: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, mi := range doc.ProfileBody.ApplicationProcess.ModuleList.ModuleItems {
		if v, err := gsdml.ParseUint32(mi.ModuleIdentNumber); err == nil && v == 0x40 {
			mi.UseableSubmodules = &gsdml.UseableSubmodules{SubmoduleItemRefs: []*gsdml.SubmoduleItemRef{
				{SubmoduleItemTarget: "SM_CHANNEL", AllowedInSubslots: "2..3", UsedInSubslots: "2"}}}
			found = true
		}
	}
	if !found {
		t.Fatal("no module 0x40 in the generated file")
	}
	b, err := gsdml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return writeFile(t, "omitted.xml", b)
}

// A module the catalog leaves out: gsdml modules lists it apart with why,
// and --plug of it fails with that reason (unsupported_gsdml), in connect
// and plan, with and without --plug-any-slot.
func TestOmittedModule(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	gf := omittedGSDMLFile(t)
	var mods modulesResult
	n.json(&mods, "gsdml", "modules", gf)
	if len(mods.Modules) != 3 || len(mods.Omitted) != 1 || mods.Omitted[0].IdentNumber != 0x40 || mods.Omitted[0].ID == "" ||
		!strings.Contains(mods.Omitted[0].Reason, "configurable submodule SM_CHANNEL") {
		t.Fatalf("modules %+v", mods)
	}
	for _, m := range mods.Modules {
		if m.IdentNumber == 0x40 {
			t.Fatalf("0x40 among the usable modules: %+v", mods)
		}
	}
	if out := n.ok("gsdml", "modules", gf); !strings.Contains(out, "3 module(s) usable") || !strings.Contains(out, "1 module(s) of the file not in the catalog (cannot be plugged):") ||
		!strings.Contains(out, "0x00000040  "+mods.Omitted[0].ID+": ") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("gsdml", "modules", gf, "-o", "table"); !strings.Contains(out, "omitted") {
		t.Fatalf("table:\n%s", out)
	}
	// The file without it lists none.
	var plain modulesResult
	n.json(&plain, "gsdml", "modules", gsdmlFile(t))
	if len(plain.Omitted) != 0 {
		t.Fatalf("plain %+v", plain)
	}
	base := []string{"-i", "pn0", "connect", "-s", "dev-a", "--gsdml", gf, "--duration", "100ms"}
	for _, extra := range [][]string{nil, {"--plug-any-slot"}} {
		msg := n.fails(3, "unsupported_gsdml", append(append(append([]string{}, base...), "--plug", "3=0x40"), extra...)...)
		if !strings.Contains(msg, `--plug "3=0x40": module `+mods.Omitted[0].ID+` (0x00000040) is not in the catalog`) || !strings.Contains(msg, "configurable submodule SM_CHANNEL") {
			t.Fatalf("connect %v: %s", extra, msg)
		}
	}
	if msg := n.fails(3, "unsupported_gsdml", "gsdml", "plan", gf, "--plug", "3=0x40"); !strings.Contains(msg, "is not in the catalog") {
		t.Fatalf("plan: %s", msg)
	}
	// An ident the file does not have stays unknown.
	if msg := n.fails(3, "invalid_input", append(append([]string{}, base...), "--plug", "3=0x99")...); !strings.Contains(msg, "no module type with ident number 0x00000099") {
		t.Fatalf("unknown: %s", msg)
	}
}

// omittedModules picks the named device access point, and none without a
// document or for one that does not parse into reports.
func TestOmittedModulesOf(t *testing.T) {
	t.Parallel()
	if omittedModules(nil, "") != nil || omittedModules(&gsdml.Document{}, "") != nil {
		t.Fatal("no document")
	}
	doc, err := gsdml.FromModel(testModel(), gsdml.GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := omittedModules(doc, "no such DAP"); got != nil {
		t.Fatalf("%+v", got)
	}
}
