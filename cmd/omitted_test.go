// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"

	"github.com/otfabric/go-profinet/device"
	"github.com/otfabric/go-profinet/gsdml"
)

// configurableDoc is the test model's GSDML file with configurable
// submodules: a SubmoduleList with SM_WIDE (4 octets in) and SM_ALT (2
// octets out), and the catalog module 0x40 allowed in slots 3 to 5 (of
// PhysicalSlots 0..5; a single allowed slot would plug it; slot 3 has a
// module the file fixes), where it uses SM_WIDE in subslot 2 by default and
// allows either in subslots 2 and 3. With inSlot1, the plugged module 0x10
// uses SM_WIDE in subslot 2 too, which gsdml.ToModel refuses.
func configurableDoc(t *testing.T, inSlot1 bool) *gsdml.Document {
	t.Helper()
	doc, err := gsdml.FromModel(testModel(), gsdml.GenerateOptions{SupervisorAR: true})
	if err != nil {
		t.Fatal(err)
	}
	ap := doc.ProfileBody.ApplicationProcess
	var text string
	items := map[uint32]*gsdml.ModuleItem{}
	for _, mi := range ap.ModuleList.ModuleItems {
		v, err := gsdml.ParseUint32(mi.ModuleIdentNumber)
		if err != nil {
			t.Fatal(err)
		}
		items[v] = mi
		text = mi.ModuleInfo.Name.TextID
	}
	info := func() *gsdml.ModuleInfo {
		return &gsdml.ModuleInfo{Name: &gsdml.Name{TextID: text}, InfoText: &gsdml.InfoText{TextID: text}}
	}
	ap.SubmoduleList = &gsdml.SubmoduleList{SubmoduleItems: []*gsdml.SubmoduleItem{
		{ID: "SM_WIDE", SubmoduleIdentNumber: "0x00000042", MayIssueProcessAlarm: "false", ModuleInfo: info(),
			IOData: &gsdml.IOData{Input: &gsdml.Input{DataItems: []*gsdml.DataItem{{DataType: "Unsigned32", TextID: text}}}}},
		{ID: "SM_ALT", SubmoduleIdentNumber: "0x00000043", MayIssueProcessAlarm: "false", ModuleInfo: info(),
			IOData: &gsdml.IOData{Output: &gsdml.Output{DataItems: []*gsdml.DataItem{{DataType: "Unsigned16", TextID: text}}}}},
	}}
	items[0x40].UseableSubmodules = &gsdml.UseableSubmodules{SubmoduleItemRefs: []*gsdml.SubmoduleItemRef{
		{SubmoduleItemTarget: "SM_WIDE", AllowedInSubslots: "2..3", UsedInSubslots: "2"},
		{SubmoduleItemTarget: "SM_ALT", AllowedInSubslots: "2..3"}}}
	if inSlot1 {
		items[0x10].UseableSubmodules = &gsdml.UseableSubmodules{SubmoduleItemRefs: []*gsdml.SubmoduleItemRef{
			{SubmoduleItemTarget: "SM_WIDE", AllowedInSubslots: "2", UsedInSubslots: "2"}}}
	}
	dap := ap.DeviceAccessPointList.DeviceAccessPointItems[0]
	dap.PhysicalSlots = "0..5"
	for _, ref := range dap.UseableModules.ModuleItemRefs {
		if ref.ModuleItemTarget == items[0x40].ID {
			ref.AllowedInSlots = "3..5"
		}
	}
	return doc
}

func docFile(t *testing.T, name string, doc *gsdml.Document) string {
	t.Helper()
	b, err := gsdml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return writeFile(t, name, b)
}

// configModel is the model gsdml config resolves, as JSON gives it.
func configModel(t *testing.T, n *testNet, args ...string) configResult {
	t.Helper()
	var r configResult
	n.json(&r, append([]string{"gsdml", "config"}, args...)...)
	return r
}

func slotOf(r configResult, slot uint16) (configSlot, bool) {
	for _, s := range r.Slots {
		if s.Slot == slot {
			return s, true
		}
	}
	return configSlot{}, false
}

func subslotsOf(s configSlot) string {
	var b []string
	for _, sm := range s.Submodules {
		b = append(b, strings.TrimSpace(sm.ID+"@"+string(rune('0'+sm.Subslot))))
	}
	return strings.Join(b, " ")
}

// gsdml modules lists a module with configurable submodules among the
// usable ones, with their number; gsdml config shows the default
// configuration, what --plug and --plug-submodule make of it, and the
// choices.
func TestGSDMLConfig(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	gf := docFile(t, "configurable.xml", configurableDoc(t, false))
	var mods modulesResult
	n.json(&mods, "gsdml", "modules", gf)
	if len(mods.Modules) != 4 || len(mods.Omitted) != 0 {
		t.Fatalf("modules %+v", mods)
	}
	for _, m := range mods.Modules {
		if m.Configurable != map[uint32]int{0x40: 2}[m.IdentNumber] {
			t.Fatalf("module %+v", m)
		}
		if m.IdentNumber == 0x40 && (m.Submodules != 2 || m.InputLen != 20) {
			t.Fatalf("0x40 with its default %+v", m)
		}
	}
	if out := n.ok("gsdml", "modules", gf); !strings.Contains(out, "2 submodule(s), 2 configurable, 20 in / 0 out octets") {
		t.Fatalf("text:\n%s", out)
	}
	// The default configuration: the plugged modules, no choices but the
	// DAP's (none).
	r := configModel(t, n, gf)
	if len(r.Slots) != 4 || len(r.Choices) != 0 {
		t.Fatalf("default %+v", r)
	}
	// Module 0x40 in slot 4: SM_WIDE in subslot 2 by default; SM_ALT
	// chosen in subslot 3, by ID and by ident number; subslot 2 cleared.
	r = configModel(t, n, gf, "--plug", "4=0x40")
	if s, _ := slotOf(r, 4); s.IdentNumber != 0x40 || subslotsOf(s) != "@1 SM_WIDE@2" || len(r.Choices) != 2 || r.Choices[1].ID != "SM_ALT" || r.Choices[1].Allowed != "2..3" {
		t.Fatalf("plugged %+v", r)
	}
	for _, alt := range []string{"SM_ALT", "0x43"} {
		r = configModel(t, n, gf, "--plug", "4=0x40", "--plug-submodule", "4:3="+alt, "--plug-submodule", "4:2=-")
		if s, _ := slotOf(r, 4); subslotsOf(s) != "@1 SM_ALT@3" {
			t.Fatalf("%s: %+v", alt, s)
		}
	}
	out := n.ok("gsdml", "config", gf, "--plug", "4=0x40")
	if !strings.Contains(out, "slot 4  0x00000040") || !strings.Contains(out, "[SM_WIDE]") || !strings.Contains(out, "configurable submodules (--plug-submodule SLOT:SUBSLOT=ID):") ||
		!strings.Contains(out, "slot 4  SM_ALT  0x00000043") || !strings.Contains(out, "(allowed 2..3, used 2)") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("gsdml", "config", gf); !strings.Contains(out, "no configurable submodules") {
		t.Fatalf("text without choices:\n%s", out)
	}
	if out := n.ok("gsdml", "config", gf, "--plug", "4=0x40", "-o", "table"); !strings.Contains(out, "SM_WIDE") || !strings.Contains(out, "SUBMODULE") {
		t.Fatalf("table:\n%s", out)
	}
	// Choices the file does not allow, and flags that do not parse.
	for _, tc := range []struct {
		code     string
		args     []string
		contains string
	}{
		{"invalid_input", []string{"--plug", "4=0x40", "--plug-submodule", "4:5=SM_ALT"}, "not allowed in subslot 5"},
		{"invalid_input", []string{"--plug", "4=0x40", "--plug-submodule", "4:2=SM_NONE"}, "SM_NONE"},
		{"invalid_input", []string{"--plug-submodule", "4:2=SM_ALT"}, "slot 4 holds no module"},
		{"invalid_input", []string{"--plug", "3=0x40"}, "fixed there"},
		{"invalid_input", []string{"--plug", "4=0x99"}, "0x00000099"},
		{"invalid_input", []string{"--plug", "x"}, "want SLOT=IDENT"},
		{"invalid_input", []string{"--plug-submodule", "4=SM_ALT"}, "want SLOT:SUBSLOT=SUBMODULE"},
		{"invalid_input", []string{"--plug-submodule", "4:0=SM_ALT"}, "want SLOT:SUBSLOT=SUBMODULE"},
		{"invalid_input", []string{"--plug-submodule", "4:2="}, "want SLOT:SUBSLOT=SUBMODULE"},
	} {
		if msg := n.fails(3, tc.code, append([]string{"gsdml", "config", gf}, tc.args...)...); !strings.Contains(msg, tc.contains) {
			t.Fatalf("%v: %s", tc.args, msg)
		}
	}
	n.fails(3, "input_file_open_failed", "gsdml", "config", "/no/such/file.xml")
}

// Two submodules with one ident number: --plug-submodule needs the ID.
func TestPlugSubmoduleAmbiguousIdent(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	doc := configurableDoc(t, false)
	doc.ProfileBody.ApplicationProcess.SubmoduleList.SubmoduleItems[1].SubmoduleIdentNumber = "0x00000042"
	gf := docFile(t, "ambiguous.xml", doc)
	if msg := n.fails(3, "invalid_input", "gsdml", "config", gf, "--plug", "4=0x40", "--plug-submodule", "4:3=0x42"); !strings.Contains(msg, "SM_WIDE and SM_ALT have SubmoduleIdentNumber 0x00000042: give the ID") {
		t.Fatal(msg)
	}
	r := configModel(t, n, gf, "--plug", "4=0x40", "--plug-submodule", "4:3=SM_ALT")
	// Which submodule is in a subslot shows in its IO data: gsdml config
	// names neither, as both have the ident number.
	if s, _ := slotOf(r, 4); subslotsOf(s) != "@1 @2 @3" || s.Submodules[1].InputLen != 4 || s.Submodules[2].OutputLen != 2 {
		t.Fatalf("%+v", s)
	}
}

// connect and plan build the model with the configuration for
// --plug-submodule and for a --plug of a module with configurable
// submodules; the AR runs against a device of that model.
func TestConnectPlugSubmodule(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	doc := configurableDoc(t, false)
	gf := docFile(t, "configurable.xml", doc)
	c, err := gsdml.NewConfiguration(doc, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.PlugIdent(4, 0x40); err != nil {
		t.Fatal(err)
	}
	if err := c.Select(4, 3, "SM_ALT"); err != nil {
		t.Fatal(err)
	}
	m, err := c.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	n.device("dev-c", "192.168.0.11", func(cfg *device.Config) { cfg.Model = m })
	out := n.ok("-i", "pn0", "connect", "-s", "dev-c", "--gsdml", gf, "--plug", "4=0x40", "--plug-submodule", "4:3=SM_ALT",
		"--output", "4:3:0102", "--duration", "500ms")
	if !strings.Contains(out, "summary: state Running") || !strings.Contains(out, "input slot 4 subslot 0x0002") || strings.Contains(out, "module_diff") {
		t.Fatalf("connect:\n%s", out)
	}
	// --plug of the module alone takes the configuration too; the plan
	// writes the record of slot 2.
	for _, extra := range [][]string{nil, {"--plug-submodule", "4:3=SM_ALT"}} {
		var p planResult
		n.json(&p, append([]string{"gsdml", "plan", gf, "--plug", "4=0x40", "--param-defaults"}, extra...)...)
		if !p.Planned || len(p.Writes) != 1 || p.Writes[0].Slot != 2 || p.Writes[0].Index != 0x10 {
			t.Fatalf("plan %v: %+v", extra, p)
		}
	}
	// The configuration plugs only where the file allows: no
	// --plug-any-slot; a model in JSON has no configurable submodules.
	if msg := n.fails(2, "invalid_flag_combination", "-i", "pn0", "connect", "-s", "dev-c", "--gsdml", gf, "--plug", "4=0x40", "--plug-any-slot"); !strings.Contains(msg, "--plug-any-slot") {
		t.Fatal(msg)
	}
	if msg := n.fails(2, "invalid_flag_combination", "-i", "pn0", "connect", "-s", "dev-c", "--model", modelFile(t), "--plug-submodule", "1:2=SM_WIDE"); !strings.Contains(msg, "needs --gsdml") {
		t.Fatal(msg)
	}
	if msg := n.fails(3, "invalid_input", "gsdml", "plan", gf, "--plug", "4=0x40", "--plug-submodule", "4:9=SM_ALT"); !strings.Contains(msg, "--plug-submodule") {
		t.Fatal(msg)
	}
	// Without configurable submodules, --plug-any-slot keeps ToModel's
	// model, as before.
	if msg := n.fails(3, "invalid_input", "-i", "pn0", "connect", "-s", "dev-c", "--gsdml", gf, "--plug", "9=0x30"); !strings.Contains(msg, "--plug-any-slot plugs it anyway") {
		t.Fatal(msg)
	}
}

// A device access point gsdml.ToModel refuses for a configurable
// submodule in a plugged module: its default configuration is the model
// of gsdml modules, gsdml submodules and connect.
func TestRefusedDAPDefaultConfiguration(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	doc := configurableDoc(t, true)
	if _, err := gsdml.ToModel(doc, ""); err == nil {
		t.Fatal("ToModel converts it")
	}
	gf := docFile(t, "refused.xml", doc)
	var mods modulesResult
	n.json(&mods, "gsdml", "modules", gf)
	if len(mods.Modules) != 4 || mods.Modules[0].Configurable != 1 || mods.Modules[0].InputLen != 12 {
		t.Fatalf("modules %+v", mods)
	}
	var subs submodulesResult
	n.json(&subs, "gsdml", "submodules", gf, "--module", "0x10")
	if len(subs.Submodules) != 2 || subs.Submodules[1].IdentNumber != 0x42 {
		t.Fatalf("submodules %+v", subs)
	}
	c, err := gsdml.NewConfiguration(doc, "")
	if err != nil {
		t.Fatal(err)
	}
	m, err := c.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	n.device("dev-r", "192.168.0.12", func(cfg *device.Config) { cfg.Model = m })
	if out := n.ok("-i", "pn0", "connect", "-s", "dev-r", "--gsdml", gf, "--duration", "500ms"); !strings.Contains(out, "summary: state Running") || !strings.Contains(out, "input slot 1 subslot 0x0002") {
		t.Fatalf("connect:\n%s", out)
	}
	// inspect and check convert it as --gsdml does.
	var ins inspectResult
	n.json(&ins, "gsdml", "inspect", gf)
	if len(ins.DAPs) != 1 || ins.DAPs[0].Error != "" || ins.DAPs[0].PlugSlots != 3 || ins.DAPs[0].Modules != 4 {
		t.Fatalf("inspect %+v", ins.DAPs)
	}
	var chk checkResult
	out, _, _ := n.run("gsdml", "check", gf, "--format", "json")
	decode(t, out, &chk)
	for _, i := range chk.Issues {
		if i.Rule == ruleModel {
			t.Fatalf("check %+v", i)
		}
	}
	// A subslot the default fills, left empty, is a choice: the model is
	// that of the file without it.
	r := configModel(t, n, gf, "--plug-submodule", "1:2=-")
	if s, _ := slotOf(r, 1); subslotsOf(s) != "@1" {
		t.Fatalf("%+v", s)
	}
}

// A default configuration that needs a choice (two submodules used in
// one subslot): --gsdml says how to choose, check reports it unvalidated,
// and --plug-submodule makes the choice.
func TestDefaultConfigurationNeedsChoice(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	doc := configurableDoc(t, true)
	for _, mi := range doc.ProfileBody.ApplicationProcess.ModuleList.ModuleItems {
		if mi.UseableSubmodules != nil && len(mi.UseableSubmodules.SubmoduleItemRefs) == 1 {
			mi.UseableSubmodules.SubmoduleItemRefs = append(mi.UseableSubmodules.SubmoduleItemRefs,
				&gsdml.SubmoduleItemRef{SubmoduleItemTarget: "SM_ALT", AllowedInSubslots: "2", UsedInSubslots: "2"})
		}
	}
	gf := docFile(t, "choice.xml", doc)
	if msg := n.fails(3, "invalid_input", "gsdml", "modules", gf); !strings.Contains(msg, "--plug-submodule SLOT:SUBSLOT=ID chooses one") {
		t.Fatal(msg)
	}
	if msg := n.fails(3, "invalid_input", "-i", "pn0", "connect", "-s", "x", "--gsdml", gf, "--duration", "100ms"); !strings.Contains(msg, "SM_WIDE") {
		t.Fatal(msg)
	}
	var chk checkResult
	out, _, _ := n.run("gsdml", "check", gf, "--format", "json")
	decode(t, out, &chk)
	found := false
	for _, i := range chk.Issues {
		if i.Rule == ruleModel {
			found = i.Severity == gsdml.SeverityUnvalidated
		}
	}
	if !found {
		t.Fatalf("check %+v", chk.Issues)
	}
	var ins inspectResult
	n.json(&ins, "gsdml", "inspect", gf)
	if ins.DAPs[0].Error == "" {
		t.Fatalf("inspect %+v", ins.DAPs)
	}
	r := configModel(t, n, gf, "--plug-submodule", "1:2=SM_ALT")
	if s, _ := slotOf(r, 1); subslotsOf(s) != "@1 SM_ALT@2" {
		t.Fatalf("%+v", s)
	}
}

// A module whose configurable submodule the file does not define: gsdml
// modules lists it apart with why, and --plug of it fails, in connect and
// plan.
func TestOmittedModule(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	doc := configurableDoc(t, false)
	doc.ProfileBody.ApplicationProcess.SubmoduleList = nil
	gf := docFile(t, "omitted.xml", doc)
	var mods modulesResult
	n.json(&mods, "gsdml", "modules", gf)
	if len(mods.Modules) != 3 || len(mods.Omitted) != 1 || mods.Omitted[0].IdentNumber != 0x40 || mods.Omitted[0].ID == "" || !strings.Contains(mods.Omitted[0].Reason, "SM_WIDE") {
		t.Fatalf("modules %+v", mods)
	}
	if out := n.ok("gsdml", "modules", gf); !strings.Contains(out, "3 module(s) usable") || !strings.Contains(out, "1 module(s) of the file not in the catalog (cannot be plugged):") ||
		!strings.Contains(out, "0x00000040  "+mods.Omitted[0].ID+": ") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("gsdml", "modules", gf, "-o", "table"); !strings.Contains(out, "omitted") {
		t.Fatalf("table:\n%s", out)
	}
	// The file without configurable submodules lists none.
	var plain modulesResult
	n.json(&plain, "gsdml", "modules", gsdmlFile(t))
	if len(plain.Omitted) != 0 {
		t.Fatalf("plain %+v", plain)
	}
	base := []string{"-i", "pn0", "connect", "-s", "dev-a", "--gsdml", gf, "--duration", "100ms", "--plug", "4=0x40"}
	if msg := n.fails(3, "invalid_gsdml", base...); !strings.Contains(msg, "SM_WIDE") {
		t.Fatalf("connect: %s", msg)
	}
	if msg := n.fails(3, "", "gsdml", "plan", gf, "--plug", "4=0x40"); !strings.Contains(msg, "SM_WIDE") {
		t.Fatalf("plan: %s", msg)
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
