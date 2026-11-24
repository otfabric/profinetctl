// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/otfabric/go-profinet"
	"github.com/otfabric/go-profinet/gsdml"
	"github.com/otfabric/go-profinet/model"
	"github.com/otfabric/profinetctl/internal/errs"
)

// modelChoices are the changes to the default configuration of a device
// access point: --plug, --plug-submodule and --plug-any-slot.
type modelChoices struct {
	plugs   []string
	subs    []string
	anySlot bool
}

// parsedPlug is a --plug SLOT=IDENT.
type parsedPlug struct {
	text  string
	slot  uint16
	ident uint32
}

func parsePlug(pl string) (parsedPlug, error) {
	slotS, identS, ok := strings.Cut(pl, "=")
	slot, err := parseUint16(slotS)
	if !ok || err != nil || slot == 0 {
		return parsedPlug{}, errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--plug %q: want SLOT=IDENT with a slot from 1", pl), err)
	}
	ident, err := strconv.ParseUint(identS, 0, 32)
	if err != nil {
		return parsedPlug{}, errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--plug %q: %v", pl, err), err)
	}
	return parsedPlug{text: pl, slot: slot, ident: uint32(ident)}, nil
}

// parsedSub is a --plug-submodule SLOT:SUBSLOT=SUBMODULE; submodule "-"
// clears the subslot.
type parsedSub struct {
	text          string
	slot, subslot uint16
	submodule     string
}

func parsePlugSubmodule(s string) (parsedSub, error) {
	addr, sub, ok := strings.Cut(s, "=")
	slotS, subslotS, ok2 := strings.Cut(addr, ":")
	slot, e1 := parseUint16(slotS)
	subslot, e2 := parseUint16(subslotS)
	sub = strings.TrimSpace(sub)
	if !ok || !ok2 || e1 != nil || e2 != nil || subslot == 0 || sub == "" {
		return parsedSub{}, errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--plug-submodule %q: want SLOT:SUBSLOT=SUBMODULE (the SubmoduleItem ID or its ident number, or - to leave the subslot empty) with a subslot from 1", s), errors.Join(e1, e2))
	}
	return parsedSub{text: s, slot: slot, subslot: subslot, submodule: sub}, nil
}

// buildModel reads the expected configuration of a device: a GSDML file
// (with the ID of its device access point) or a model in JSON, with the
// modules of --plug and the submodules of --plug-submodule.
//
// A GSDML file becomes gsdml.ToModel's model, --plug changing its slots,
// unless it needs go-profinet's gsdml.Configuration: --plug-submodule is
// given, ToModel refuses the device access point (ErrUnsupported: it needs
// configurable submodules), or a --plug names a module ToModel leaves out
// of the catalog for one. The configuration plugs modules only in the
// slots the file allows (--plug-any-slot is refused) and does not replace a
// module the file fixes in a slot.
func buildModel(gsdmlFile, dap, modelFile string, ch modelChoices) (*model.Device, *gsdml.Document, error) {
	if gsdmlFile == "" || modelFile != "" {
		if len(ch.subs) > 0 && modelFile != "" && gsdmlFile == "" {
			return nil, nil, errs.New(errs.KindUsage, errs.CodeInvalidFlagCombination, "--plug-submodule needs --gsdml: a model in JSON has its submodules", nil)
		}
		m, doc, err := loadModelDoc(gsdmlFile, dap, modelFile)
		if err != nil {
			return nil, nil, err
		}
		if err := plug(m, ch.plugs, nil, nil); err != nil {
			return nil, nil, err
		}
		return m, doc, nil
	}
	doc, err := readGSDML(gsdmlFile)
	if err != nil {
		return nil, nil, err
	}
	plugs := make([]parsedPlug, 0, len(ch.plugs))
	for _, pl := range ch.plugs {
		p, err := parsePlug(pl)
		if err != nil {
			return nil, nil, err
		}
		plugs = append(plugs, p)
	}
	m, terr := gsdml.ToModel(doc, dap)
	var omitted []omittedModule
	if terr == nil && len(plugs) > 0 {
		omitted = omittedModules(doc, dap)
	}
	if len(ch.subs) == 0 && !errors.Is(terr, profinet.ErrUnsupported) && !plugsOmitted(plugs, omitted) {
		if terr != nil {
			return nil, nil, gsdmlError(fmt.Sprintf("%s: %v", gsdmlFile, terr), terr)
		}
		var rules *slotRules
		if !ch.anySlot {
			rules = slotRulesOf(doc, dap)
		}
		if err := plug(m, ch.plugs, rules, omitted); err != nil {
			return nil, nil, err
		}
		return m, doc, nil
	}
	if ch.anySlot && len(plugs) > 0 {
		return nil, nil, errs.New(errs.KindUsage, errs.CodeInvalidFlagCombination,
			"--plug-any-slot: this configuration needs configurable submodules (go-profinet's gsdml.Configuration), which plugs modules only in the slots the GSDML file allows", nil)
	}
	_, m, err = configure(gsdmlFile, doc, dap, plugs, ch.subs)
	if err != nil {
		return nil, nil, err
	}
	return m, doc, nil
}

// defaultModel is the model of a device access point that --gsdml with
// its ID and no choices gives: gsdml.ToModel's, or for one ToModel refuses
// as unsupported, its default configuration's (gsdml.Configuration). The
// configuration's error replaces ToModel's when it is not unsupported too.
func defaultModel(doc *gsdml.Document, dap string) (*model.Device, error) {
	m, err := gsdml.ToModel(doc, dap)
	if !errors.Is(err, profinet.ErrUnsupported) {
		return m, err
	}
	c, cerr := gsdml.NewConfiguration(doc, dap)
	if cerr == nil {
		var cm *model.Device
		if cm, cerr = c.Resolve(); cerr == nil {
			return cm, nil
		}
	}
	if errors.Is(cerr, profinet.ErrUnsupported) {
		return nil, err
	}
	return nil, cerr
}

// plugsOmitted reports whether a --plug names a module the catalog leaves
// out.
func plugsOmitted(plugs []parsedPlug, omitted []omittedModule) bool {
	for _, p := range plugs {
		for _, om := range omitted {
			if om.IdentNumber == p.ident {
				return true
			}
		}
	}
	return false
}

// configure is the configuration of a device access point with the
// modules of plugs, then the submodules of subs, and its model.
func configure(file string, doc *gsdml.Document, dap string, plugs []parsedPlug, subs []string) (*gsdml.Configuration, *model.Device, error) {
	c, err := gsdml.NewConfiguration(doc, dap)
	if err != nil {
		return nil, nil, gsdmlError(fmt.Sprintf("%s: %v", file, err), err)
	}
	for _, p := range plugs {
		if err := c.PlugIdent(p.slot, p.ident); err != nil {
			return nil, nil, choiceError("--plug", p.text, err)
		}
	}
	for _, s := range subs {
		ps, err := parsePlugSubmodule(s)
		if err != nil {
			return nil, nil, err
		}
		if ps.submodule == "-" {
			err = c.ClearSubslot(ps.slot, ps.subslot)
		} else {
			var id string
			if id, err = submoduleID(c, ps.slot, ps.submodule); err == nil {
				err = c.Select(ps.slot, ps.subslot, id)
			}
		}
		if err != nil {
			return nil, nil, choiceError("--plug-submodule", s, err)
		}
	}
	m, err := c.Resolve()
	if err != nil {
		msg := fmt.Sprintf("%s: %v", file, err)
		if errors.Is(err, profinet.ErrInvalidConfig) {
			return nil, nil, errs.InvalidInput(errs.CodeInvalidInput, msg+" (profinetctl gsdml config lists the configurable submodules; --plug-submodule SLOT:SUBSLOT=ID chooses one)", err)
		}
		return nil, nil, gsdmlError(msg, err)
	}
	return c, m, nil
}

// choiceError is the typed error of a choice the configuration refused:
// invalid_input for one the file does not allow, unsupported_gsdml or
// invalid_gsdml for one go-profinet cannot convert.
func choiceError(flag, value string, err error) error {
	msg := fmt.Sprintf("%s %q: %v", flag, value, err)
	if errors.Is(err, profinet.ErrInvalidConfig) && !errors.Is(err, profinet.ErrUnsupported) && !errors.Is(err, profinet.ErrMalformed) {
		return errs.InvalidInput(errs.CodeInvalidInput, msg, err)
	}
	return gsdmlError(msg, err)
}

// slotOptions are the configurable submodules of slot: the DAP's for slot
// 0, those of the module plugged in it otherwise; and the module's ID.
func slotOptions(c *gsdml.Configuration, slot uint16) (string, []gsdml.SubmoduleOption, *model.Module) {
	if slot == 0 {
		return "", c.DAP(), nil
	}
	id, ok := c.Module(slot)
	mods := c.Modules()
	i := slices.IndexFunc(mods, func(mo gsdml.ModuleOption) bool { return mo.ID == id })
	if !ok || i < 0 {
		return id, nil, nil
	}
	return id, mods[i].Submodules, &mods[i].Module
}

// submoduleID is the SubmoduleItem ID a --plug-submodule names: an ID of
// the slot's configurable submodules, else the ID of the only one with that
// ident number. Anything else is returned as given, for Select to refuse.
func submoduleID(c *gsdml.Configuration, slot uint16, v string) (string, error) {
	_, opts, _ := slotOptions(c, slot)
	for _, o := range opts {
		if o.ID == v {
			return v, nil
		}
	}
	// Not an ID: an ident number names the submodule that has it.
	if ident, err := gsdml.ParseUint32(v); err == nil {
		var ids []string
		for _, o := range opts {
			if o.Submodule.IdentNumber == ident && !slices.Contains(ids, o.ID) {
				ids = append(ids, o.ID)
			}
		}
		switch len(ids) {
		case 0:
		case 1:
			return ids[0], nil
		default:
			return "", fmt.Errorf("%w: submodules %s have SubmoduleIdentNumber 0x%08X: give the ID", profinet.ErrInvalidConfig, strings.Join(ids, " and "), ident)
		}
	}
	return v, nil
}

// configResult is the configuration of a device access point: its slots
// as resolved, and the configurable submodules of the DAP and of each
// plugged module.
type configResult struct {
	DAP     string         `json:"dap"`
	Slots   []configSlot   `json:"slots"`
	Choices []configChoice `json:"choices"`
}

type configSlot struct {
	Slot        uint16            `json:"slot"`
	Module      string            `json:"module"`
	IdentNumber uint32            `json:"ident_number"`
	Submodules  []configSubmodule `json:"submodules"`
}

type configSubmodule struct {
	Subslot     uint16 `json:"subslot"`
	Name        string `json:"name"`
	IdentNumber uint32 `json:"ident_number"`
	API         uint32 `json:"api"`
	InputLen    uint16 `json:"input_length"`
	OutputLen   uint16 `json:"output_length"`
	Records     int    `json:"records"`
	// ID is the SubmoduleItem of a configurable submodule; absent for a
	// built-in one.
	ID string `json:"id,omitempty"`
}

// configChoice is a configurable submodule of the DAP (slot 0) or of a
// plugged module: what --plug-submodule can put in that slot.
type configChoice struct {
	Slot        uint16 `json:"slot"`
	Module      string `json:"module"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	IdentNumber uint32 `json:"ident_number"`
	Port        bool   `json:"port,omitempty"`
	Allowed     string `json:"allowed_in_subslots,omitempty"`
	Fixed       string `json:"fixed_in_subslots,omitempty"`
	Used        string `json:"used_in_subslots,omitempty"`
	Error       string `json:"error,omitempty"`
}

func newConfigResult(c *gsdml.Configuration, m *model.Device, dap string) configResult {
	r := configResult{DAP: orDash(dap), Slots: []configSlot{}, Choices: []configChoice{}}
	add := func(slot uint16, mod model.Module) {
		modID, opts, builtin := slotOptions(c, slot)
		if slot == 0 {
			modID = mod.Name
		}
		s := configSlot{Slot: slot, Module: mod.Name, IdentNumber: mod.IdentNumber, Submodules: []configSubmodule{}}
		for _, sm := range mod.Submodules {
			v := configSubmodule{Subslot: sm.Subslot, Name: sm.Name, IdentNumber: sm.IdentNumber, API: sm.API,
				InputLen: sm.InputLen, OutputLen: sm.OutputLen, Records: len(sm.Records), ID: configurableID(opts, builtin, sm)}
			s.Submodules = append(s.Submodules, v)
		}
		r.Slots = append(r.Slots, s)
		for _, o := range opts {
			r.Choices = append(r.Choices, configChoice{Slot: slot, Module: modID, ID: o.ID, Name: o.Submodule.Name, IdentNumber: o.Submodule.IdentNumber,
				Port: o.Port, Allowed: o.AllowedInSubslots, Fixed: o.FixedInSubslots, Used: o.UsedInSubslots, Error: o.Error})
		}
	}
	add(0, m.DAP)
	for _, sl := range m.Slots {
		add(sl.Number, sl.Module)
	}
	return r
}

// configurableID is the ID of the configurable submodule sm is: the only
// option with its ident number that allows its subslot, which is not a
// subslot of the module's built-in submodules.
func configurableID(opts []gsdml.SubmoduleOption, builtin *model.Module, sm model.Submodule) string {
	if builtin != nil {
		for _, b := range builtin.Submodules {
			if b.Subslot == sm.Subslot {
				return ""
			}
		}
	}
	id := ""
	for _, o := range opts {
		if o.Err != nil || o.Submodule.IdentNumber != sm.IdentNumber || !o.Allows(sm.Subslot) {
			continue
		}
		if id != "" && id != o.ID {
			return ""
		}
		id = o.ID
	}
	return id
}

func (r configResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, s := range r.Slots {
		fmt.Fprintf(&b, "slot %d  0x%08X  %s\n", s.Slot, s.IdentNumber, s.Module)
		for _, sm := range s.Submodules {
			id := ""
			if sm.ID != "" {
				id = "  [" + sm.ID + "]"
			}
			fmt.Fprintf(&b, "  subslot 0x%04X  0x%08X  %-28s %d in / %d out octets, %d record(s)%s\n", sm.Subslot, sm.IdentNumber, sm.Name, sm.InputLen, sm.OutputLen, sm.Records, id)
		}
	}
	if len(r.Choices) == 0 {
		b.WriteString("no configurable submodules\n")
		return b.String(), nil
	}
	b.WriteString("configurable submodules (--plug-submodule SLOT:SUBSLOT=ID):\n")
	for _, c := range r.Choices {
		var where []string
		for _, w := range [][2]string{{"allowed", c.Allowed}, {"fixed", c.Fixed}, {"used", c.Used}} {
			if w[1] != "" {
				where = append(where, w[0]+" "+clipList(w[1]))
			}
		}
		line := fmt.Sprintf("  slot %d  %s  0x%08X  %s", c.Slot, c.ID, c.IdentNumber, c.Name)
		if len(where) > 0 {
			line += "  (" + strings.Join(where, ", ") + ")"
		}
		if c.Error != "" {
			line += ": cannot be placed: " + c.Error
		}
		b.WriteString(line + "\n")
	}
	return b.String(), nil
}

func (r configResult) TableHeaders() []string {
	return []string{"SLOT", "SUBSLOT", "MODULE", "SUBMODULE", "IDENT", "IN", "OUT", "ID"}
}

func (r configResult) TableRows() [][]string {
	var rows [][]string
	for _, s := range r.Slots {
		for _, sm := range s.Submodules {
			rows = append(rows, []string{fmt.Sprint(s.Slot), fmt.Sprintf("0x%04X", sm.Subslot), s.Module, sm.Name,
				fmt.Sprintf("0x%08X", sm.IdentNumber), fmt.Sprint(sm.InputLen), fmt.Sprint(sm.OutputLen), orDash(sm.ID)})
		}
	}
	return rows
}

func newGSDMLConfigCmd(a *app) *cobra.Command {
	var dap string
	var plugs, subs []string
	c := &cobra.Command{
		Use:   "config FILE [--dap ID] [--plug SLOT=IDENT]... [--plug-submodule SLOT:SUBSLOT=SUBMODULE]...",
		Short: "Show a device access point's configuration: its slots and the configurable submodules it can choose",
		Long: `Resolve the configuration of a device access point with go-profinet's
gsdml.Configuration and show it: each slot (0 is the DAP) with its module
and submodules, a configurable submodule marked with its SubmoduleItem ID,
and the configurable submodules the DAP and each plugged module can put in
their subslots, with the subslots the file allows, fixes or uses them in.

Without flags it is the default configuration: the modules the file plugs,
each configurable submodule in the subslots it is fixed in, else used in.
--plug and --plug-submodule change it as they do for an AR: --plug plugs a
module in a slot the file allows, --plug-submodule puts a configurable
submodule (its SubmoduleItem ID, or its ident number when one submodule has
it) in a subslot, or with - leaves the subslot empty. A configuration the
file does not allow (a fixed submodule replaced, a subslot two submodules
are used in by default without a choice) exits 3.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cc *cobra.Command, args []string) error {
			return a.result(cc, func(ctx context.Context) (any, error) {
				doc, err := readGSDML(args[0])
				if err != nil {
					return nil, err
				}
				var ps []parsedPlug
				for _, pl := range plugs {
					p, err := parsePlug(pl)
					if err != nil {
						return nil, err
					}
					ps = append(ps, p)
				}
				cfg, m, err := configure(args[0], doc, dap, ps, subs)
				if err != nil {
					return nil, err
				}
				return newConfigResult(cfg, m, dap), nil
			})
		},
	}
	f := c.Flags()
	f.StringVar(&dap, "dap", "", "Device access point ID (default: the first)")
	f.StringArrayVar(&plugs, "plug", nil, "Plug the module with this ident number: SLOT=IDENT (repeatable), in a slot the GSDML file allows it in")
	f.StringArrayVar(&subs, "plug-submodule", nil, plugSubmoduleUsage)
	return c
}

// plugSubmoduleUsage is the help of --plug-submodule.
const plugSubmoduleUsage = "Put a configurable submodule in a subslot: SLOT:SUBSLOT=SUBMODULE, the SubmoduleItem ID or its ident number, or - to leave the subslot empty (repeatable; slot 0 is the DAP; after --plug)"
