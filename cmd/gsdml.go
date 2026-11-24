// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/otfabric/go-profinet"
	"github.com/otfabric/go-profinet/gsdml"
	"github.com/otfabric/go-profinet/model"

	"github.com/otfabric/profinetctl/internal/errs"
)

// readGSDML parses and validates a GSDML file.
// Parse bounds the size of the document.
func readGSDML(path string) (*gsdml.Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errs.InvalidInput(errs.CodeInputFileOpenFailed, fmt.Sprintf("GSDML file: %v", err), err)
	}
	defer func() { _ = f.Close() }()
	doc, err := gsdml.Parse(f)
	if err != nil {
		return nil, gsdmlError(fmt.Sprintf("%s: %v", path, err), err)
	}
	return doc, nil
}

// gsdmlError is the typed error of a GSDML file go-profinet refused:
// unsupported_gsdml when it does not support what the file has, otherwise
// invalid_gsdml. Both are invalid input (exit code 3): nothing was sent.
func gsdmlError(msg string, err error) error {
	if errors.Is(err, profinet.ErrUnsupported) {
		return errs.InvalidInput(errs.CodeUnsupportedGSDML, msg, err)
	}
	return errs.InvalidInput(errs.CodeInvalidGSDML, msg, err)
}

// loadModel reads the expected configuration of a device: a GSDML file
// (with the ID of its device access point) or a model in JSON.
func loadModel(gsdmlFile, dap, modelFile string) (*model.Device, error) {
	m, _, err := loadModelDoc(gsdmlFile, dap, modelFile)
	return m, err
}

// loadModelDoc is loadModel, with the GSDML document the model is of: nil
// for a model in JSON.
func loadModelDoc(gsdmlFile, dap, modelFile string) (*model.Device, *gsdml.Document, error) {
	switch {
	case gsdmlFile != "" && modelFile != "":
		return nil, nil, errs.New(errs.KindUsage, errs.CodeInvalidFlagCombination, "--gsdml and --model both describe the device: give one", nil)
	case gsdmlFile != "":
		doc, err := readGSDML(gsdmlFile)
		if err != nil {
			return nil, nil, err
		}
		m, err := gsdml.ToModel(doc, dap)
		if errors.Is(err, profinet.ErrUnsupported) {
			// A device access point that needs configurable submodules:
			// its default configuration.
			_, cm, cerr := configure(gsdmlFile, doc, dap, nil, nil)
			if cerr != nil {
				return nil, nil, cerr
			}
			return cm, doc, nil
		}
		if err != nil {
			return nil, nil, gsdmlError(fmt.Sprintf("%s: %v", gsdmlFile, err), err)
		}
		return m, doc, nil
	case modelFile != "":
		b, err := readInputFile(modelFile)
		if err != nil {
			return nil, nil, errs.InvalidInput(errs.CodeInputFileOpenFailed, fmt.Sprintf("model file: %v", err), err)
		}
		var m model.Device
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, nil, errs.InvalidInput(errs.CodeInvalidModel, fmt.Sprintf("%s: %v", modelFile, err), err)
		}
		if err := m.Validate(); err != nil {
			return nil, nil, errs.InvalidInput(errs.CodeInvalidModel, fmt.Sprintf("%s: %v", modelFile, err), err)
		}
		return &m, nil, nil
	default:
		return nil, nil, errs.New(errs.KindUsage, errs.CodeInvalidInput, "no device description: give --gsdml FILE (and --dap ID) or --model FILE", nil)
	}
}

// dapView describes a device access point of a GSDML file.
type dapView struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	IdentNumber       string `json:"module_ident_number"`
	ConformanceClass  string `json:"conformance_class,omitempty"`
	MinDeviceInterval uint16 `json:"min_device_interval"`
	Ports             int    `json:"ports"`
	DeviceAccess      bool   `json:"device_access_supported"`
	PlugSlots         int    `json:"plugged_slots"`
	Modules           int    `json:"usable_modules"`
	Error             string `json:"error,omitempty"`
}

// inspectResult summarizes a GSDML file.
type inspectResult struct {
	File          string    `json:"file"`
	SchemaVersion string    `json:"schema_version,omitempty"`
	VendorID      string    `json:"vendor_id"`
	DeviceID      string    `json:"device_id"`
	VendorName    string    `json:"vendor_name"`
	Family        string    `json:"family,omitempty"`
	DAPs          []dapView `json:"device_access_points"`
	// Valid and Issues are what gsdml validate reports: the errors of the
	// structure (against Scope) and of the semantic rules that are errors.
	Valid  bool `json:"valid"`
	Issues int  `json:"issues"`
	// Scope says what the validation covers, as gsdml validate does.
	Scope string `json:"scope"`
}

func (r inspectResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "File:     %s\n", r.File)
	if r.SchemaVersion != "" {
		fmt.Fprintf(&b, "Schema:   GSDML %s\n", r.SchemaVersion)
	}
	fmt.Fprintf(&b, "Vendor:   %s (%s)\n", orDash(r.VendorName), r.VendorID)
	fmt.Fprintf(&b, "Device:   %s", r.DeviceID)
	if r.Family != "" {
		fmt.Fprintf(&b, ", family %s", r.Family)
	}
	b.WriteString("\n")
	valid := "yes"
	if !r.Valid {
		valid = fmt.Sprintf("no, %d issue(s): see profinetctl gsdml validate", r.Issues)
	}
	fmt.Fprintf(&b, "Valid:    %s\n", valid)
	fmt.Fprintf(&b, "Checked:  %s\n", r.Scope)
	fmt.Fprintf(&b, "Device access points: %d\n", len(r.DAPs))
	for _, d := range r.DAPs {
		fmt.Fprintf(&b, "  %s  %q ident %s class %s, %d port(s), %d plugged slot(s), %d usable module(s)", d.ID, d.Name, d.IdentNumber, orDash(d.ConformanceClass), d.Ports, d.PlugSlots, d.Modules)
		if d.Error != "" {
			fmt.Fprintf(&b, " (error: %s)", d.Error)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func (r inspectResult) TableHeaders() []string {
	return []string{"DAP", "NAME", "IDENT", "CLASS", "PORTS", "PLUGGED", "MODULES"}
}

func (r inspectResult) TableRows() [][]string {
	var rows [][]string
	for _, d := range r.DAPs {
		rows = append(rows, []string{d.ID, d.Name, d.IdentNumber, orDash(d.ConformanceClass), fmt.Sprint(d.Ports), fmt.Sprint(d.PlugSlots), fmt.Sprint(d.Modules)})
	}
	return rows
}

var schemaVersionRE = regexp.MustCompile(`GSDML-DeviceProfile-[vV]?([0-9.]+)\.xsd`)

// inspectDAPs is how many device access points inspect converts to a model
// for their name, ports, slots and modules; each conversion reads the whole
// document. The device access points after them are listed with their ID,
// ident number and conformance class only.
const inspectDAPs = 64

// interrupted returns the error of a context that has ended: exit code 130
// after Ctrl-C or SIGTERM. go-profinet's GSDML functions take no context, so
// the gsdml commands check it between them.
func interrupted(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return classify("GSDML", err)
	}
	return nil
}

// inspect summarizes a GSDML file. Its validity is that of gsdml validate:
// the errors of rep, the report of checkFile with against.
func inspect(ctx context.Context, path string, doc *gsdml.Document, rep gsdml.Report, against string) (inspectResult, error) {
	r := inspectResult{File: path, DAPs: []dapView{}}
	for _, at := range doc.Attrs {
		if m := schemaVersionRE.FindStringSubmatch(at.Value); m != nil {
			r.SchemaVersion = m[1]
		}
	}
	if pb := doc.ProfileBody; pb != nil {
		if id := pb.DeviceIdentity; id != nil {
			r.VendorID, r.DeviceID = id.VendorID, id.DeviceID
			if id.VendorName != nil {
				r.VendorName = id.VendorName.Value
			}
		}
		if f := pb.DeviceFunction; f != nil && f.Family != nil {
			r.Family = strings.TrimSpace(f.Family.MainFamily + " " + f.Family.ProductFamily)
		}
		if ap := pb.ApplicationProcess; ap != nil && ap.DeviceAccessPointList != nil {
			for i, d := range ap.DeviceAccessPointList.DeviceAccessPointItems {
				v := dapView{ID: d.ID, IdentNumber: d.ModuleIdentNumber, DeviceAccess: gsdml.ParseBool(d.DeviceAccessSupported, false)}
				if d.CertificationInfo != nil {
					v.ConformanceClass = d.CertificationInfo.ConformanceClass
				}
				if i >= inspectDAPs {
					v.Error = fmt.Sprintf("not converted: more than %d device access points", inspectDAPs)
					r.DAPs = append(r.DAPs, v)
					continue
				}
				if err := interrupted(ctx); err != nil {
					return inspectResult{}, err
				}
				if m, err := defaultModel(doc, d.ID); err != nil {
					v.Error = err.Error()
				} else {
					v.Name, v.MinDeviceInterval, v.Ports = m.Identity.TypeOfStation, m.Capabilities.MinDeviceInterval, m.Capabilities.Ports
					v.PlugSlots, v.Modules = len(m.Slots), len(m.ModuleTypes())
				}
				r.DAPs = append(r.DAPs, v)
			}
		}
	}
	if rep.SchemaVersion != "" {
		r.SchemaVersion = rep.SchemaVersion
	}
	r.Issues = len(validationErrors(rep))
	r.Valid = r.Issues == 0
	r.Scope = structureScope(against, rep.SchemaVersion) + validationScope
	return r, interrupted(ctx)
}

// validateResult is the outcome of a validation.
type validateResult struct {
	File          string        `json:"file"`
	SchemaVersion string        `json:"schema_version,omitempty"`
	Valid         bool          `json:"valid"`
	Issues        []gsdml.Issue `json:"issues"`
	// Scope says what the validation covers, and what it does not.
	Scope string `json:"scope"`
}

// validationErrors are the issues of a report that make a file invalid:
// what gsdml validate lists.
func validationErrors(rep gsdml.Report) []gsdml.Issue {
	out := []gsdml.Issue{}
	for _, i := range rep.Issues {
		if i.Severity == gsdml.SeverityError {
			out = append(out, i)
		}
	}
	return out
}

// validationScope follows structureScope: what validate checks besides.
const validationScope = ", whether each submodule's IO data fits an RT frame, and the errors of the parameter records (go-profinet's record rules: layout, overlaps, defaults, AllowedValues and ValueItems, TransferSequence); not PI's GSDML checker"

func (r validateResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, i := range r.Issues {
		fmt.Fprintf(&b, "%s\n", issueText(i))
	}
	if r.Valid {
		fmt.Fprintf(&b, "%s: valid\n", r.File)
	} else {
		fmt.Fprintf(&b, "%s: %d issue(s)\n", r.File, len(r.Issues))
	}
	fmt.Fprintf(&b, "checked: %s\n", r.Scope)
	return b.String(), nil
}

func (r validateResult) TableHeaders() []string { return []string{"LINE", "PATH", "ISSUE"} }

func (r validateResult) TableRows() [][]string {
	var rows [][]string
	for _, i := range r.Issues {
		rows = append(rows, []string{issueLine(i.Line), i.Path, i.Message})
	}
	return rows
}

// Partial reports a file with issues: exit code 7.
func (r validateResult) Partial() bool { return !r.Valid }

// moduleView is a module type of a device access point.
type moduleView struct {
	Name        string   `json:"name"`
	IdentNumber uint32   `json:"ident_number"`
	Slots       []uint16 `json:"plugged_in_slots,omitempty"`
	Submodules  int      `json:"submodules"`
	InputLen    int      `json:"input_length"`
	OutputLen   int      `json:"output_length"`
	Info        string   `json:"info,omitempty"`
	// Configurable is the number of configurable submodules the module can
	// choose (UseableSubmodules); Submodules and the lengths are those of
	// its default configuration.
	Configurable int `json:"configurable_submodules,omitempty"`
}

type modulesResult struct {
	DAP     string       `json:"dap"`
	Modules []moduleView `json:"modules"`
	// Omitted are the modules the file lets the DAP use that --plug
	// refuses, with why: go-profinet cannot place their configurable
	// submodules.
	Omitted []omittedModuleView `json:"omitted_modules,omitempty"`
}

// omittedModuleView is a module the catalog leaves out.
type omittedModuleView struct {
	ID          string `json:"id"`
	IdentNumber uint32 `json:"ident_number"`
	Reason      string `json:"reason"`
}

func (r modulesResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, m := range r.Modules {
		slots := "-"
		if len(m.Slots) > 0 {
			slots = strings.Trim(fmt.Sprint(m.Slots), "[]")
		}
		conf := ""
		if m.Configurable > 0 {
			conf = fmt.Sprintf(", %d configurable", m.Configurable)
		}
		fmt.Fprintf(&b, "0x%08X  %-32s slots %-8s %d submodule(s)%s, %d in / %d out octets\n", m.IdentNumber, m.Name, slots, m.Submodules, conf, m.InputLen, m.OutputLen)
	}
	fmt.Fprintf(&b, "%d module(s) usable with DAP %s\n", len(r.Modules), r.DAP)
	if len(r.Omitted) > 0 {
		fmt.Fprintf(&b, "%d module(s) of the file not in the catalog (cannot be plugged):\n", len(r.Omitted))
		for _, o := range r.Omitted {
			fmt.Fprintf(&b, "0x%08X  %s: %s\n", o.IdentNumber, o.ID, o.Reason)
		}
	}
	return b.String(), nil
}

func (r modulesResult) TableHeaders() []string {
	return []string{"IDENT", "NAME", "SLOTS", "SUBMODULES", "IN", "OUT"}
}

func (r modulesResult) TableRows() [][]string {
	var rows [][]string
	for _, m := range r.Modules {
		slots := "-"
		if len(m.Slots) > 0 {
			slots = strings.Trim(fmt.Sprint(m.Slots), "[]")
		}
		rows = append(rows, []string{fmt.Sprintf("0x%08X", m.IdentNumber), m.Name, slots, fmt.Sprint(m.Submodules), fmt.Sprint(m.InputLen), fmt.Sprint(m.OutputLen)})
	}
	for _, o := range r.Omitted {
		rows = append(rows, []string{fmt.Sprintf("0x%08X", o.IdentNumber), o.ID, "omitted", "-", "-", "-"})
	}
	return rows
}

// submoduleView is a submodule type.
type submoduleView struct {
	Module      string       `json:"module"`
	ModuleIdent uint32       `json:"module_ident_number"`
	Name        string       `json:"name"`
	IdentNumber uint32       `json:"ident_number"`
	API         uint32       `json:"api"`
	Subslot     uint16       `json:"subslot"`
	InputLen    uint16       `json:"input_length"`
	OutputLen   uint16       `json:"output_length"`
	Records     []recordView `json:"records,omitempty"`
}

// recordView is a parameter record of a submodule.
type recordView struct {
	Index    uint16 `json:"index"`
	Name     string `json:"name,omitempty"`
	Length   uint16 `json:"length"`
	Default  string `json:"default"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

func recordViews(rs []model.Record) []recordView {
	var out []recordView
	for _, r := range rs {
		d := r.Default
		if d == nil {
			d = make([]byte, r.Length)
		}
		out = append(out, recordView{Index: uint16(r.Index), Name: r.Name, Length: r.Length, Default: hex.EncodeToString(d), ReadOnly: r.ReadOnly})
	}
	return out
}

type submodulesResult struct {
	DAP        string          `json:"dap"`
	Submodules []submoduleView `json:"submodules"`
}

func (r submodulesResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, s := range r.Submodules {
		api := ""
		if s.API != 0 {
			api = fmt.Sprintf("API 0x%08X  ", s.API)
		}
		fmt.Fprintf(&b, "0x%08X/0x%08X  %ssubslot 0x%04X  %-28s %d in / %d out octets, %d record(s)\n", s.ModuleIdent, s.IdentNumber, api, s.Subslot, s.Name, s.InputLen, s.OutputLen, len(s.Records))
		for _, rec := range s.Records {
			fmt.Fprintf(&b, "    record 0x%04X %q, %d octets, default %s\n", rec.Index, rec.Name, rec.Length, rec.Default)
		}
	}
	return b.String(), nil
}

// otherAPIs reports whether a submodule of an API other than 0 is listed:
// the table then has an API column, as in device info.
func (r submodulesResult) otherAPIs() bool {
	return slices.ContainsFunc(r.Submodules, func(s submoduleView) bool { return s.API != 0 })
}

func (r submodulesResult) TableHeaders() []string {
	h := []string{"MODULE", "SUBMODULE", "SUBSLOT", "NAME", "IN", "OUT", "RECORDS"}
	if r.otherAPIs() {
		h = slices.Insert(h, 2, "API")
	}
	return h
}

func (r submodulesResult) TableRows() [][]string {
	var rows [][]string
	api := r.otherAPIs()
	for _, s := range r.Submodules {
		row := []string{fmt.Sprintf("0x%08X", s.ModuleIdent), fmt.Sprintf("0x%08X", s.IdentNumber), fmt.Sprintf("0x%04X", s.Subslot),
			s.Name, fmt.Sprint(s.InputLen), fmt.Sprint(s.OutputLen), fmt.Sprint(len(s.Records))}
		if api {
			row = slices.Insert(row, 2, fmt.Sprintf("0x%08X", s.API))
		}
		rows = append(rows, row)
	}
	return rows
}

// generateResult reports a generated GSDML file.
type generateResult struct {
	File  string `json:"file"`
	Bytes int    `json:"bytes"`
}

func (r generateResult) MarshalTextOutput() (string, error) {
	return fmt.Sprintf("wrote %s (%d octets)\n", r.File, r.Bytes), nil
}

func (r generateResult) TableHeaders() []string { return []string{"FILE", "BYTES"} }
func (r generateResult) TableRows() [][]string  { return [][]string{{r.File, fmt.Sprint(r.Bytes)}} }

func newGSDMLCmd(a *app) *cobra.Command {
	g := groupCmd("gsdml", "Inspect, validate and generate GSDML device descriptions")
	var inspectAgainst string
	inspectCmd := &cobra.Command{
		Use:   "inspect FILE",
		Short: "Summarize a GSDML file: identity, device access points, modules",
		Long: `Summarize a GSDML file: its identity, and each device access point converted
to a model (the first 64). Whether the file is valid is what gsdml validate
reports, with the same --against: the structure against the GSDML schema of the
file's version, or go-profinet's catalog. This is not PI's GSDML checker.`,
		Args:    cobra.ExactArgs(1),
		PreRunE: func(*cobra.Command, []string) error { return checkAgainst(inspectAgainst) },
		RunE: func(c *cobra.Command, args []string) error {
			return a.result(c, func(ctx context.Context) (any, error) {
				rep, doc, err := checkFile(args[0], inspectAgainst)
				if err != nil {
					return nil, err
				}
				return inspect(ctx, args[0], doc, rep, inspectAgainst)
			})
		},
	}
	inspectCmd.Flags().StringVar(&inspectAgainst, "against", againstSchema, "Validate the structure against the schema of the file's version (schema) or go-profinet's catalog (catalog)")
	var against string
	validateCmd := &cobra.Command{
		Use:   "validate FILE",
		Short: "Validate a GSDML file; exit code 7 when it has errors",
		Long: `Validate a GSDML file: its structure against the GSDML schema of the version it
names (go-profinet's validator, generated from PI's reference schemas and
checked against xmllint: the elements and attributes, their order and number,
lexical types and facets, and the identity constraints, each issue with its
line), and go-profinet's semantic rules that are errors (the input and the
output data of every submodule fit an RT frame; the parameter records can be
built: their defaults encode, no Consts share octets, every ValueItemTarget
resolves, and the TransferSequence of a submodule's records is 1 to N). A file naming no version
go-profinet has a schema for, or --against catalog, is validated against
go-profinet's version-independent catalog instead. The schemas are not part of
profinetctl, and this is not PI's GSDML checker.`,
		Args:    cobra.ExactArgs(1),
		PreRunE: func(*cobra.Command, []string) error { return checkAgainst(against) },
		RunE: func(c *cobra.Command, args []string) error {
			return a.result(c, func(ctx context.Context) (any, error) {
				rep, _, err := checkFile(args[0], against)
				if err != nil {
					return nil, err
				}
				r := validateResult{File: args[0], SchemaVersion: rep.SchemaVersion, Issues: validationErrors(rep),
					Scope: structureScope(against, rep.SchemaVersion) + validationScope}
				r.Valid = len(r.Issues) == 0
				return r, interrupted(ctx)
			})
		},
	}
	validateCmd.Flags().StringVar(&against, "against", againstSchema, "Validate the structure against the schema of the file's version (schema) or go-profinet's catalog (catalog)")
	var dap string
	modulesCmd := &cobra.Command{
		Use:   "modules FILE",
		Short: "List the module types a device access point accepts",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return a.result(c, func(ctx context.Context) (any, error) {
				m, doc, err := loadModelDoc(args[0], dap, "")
				if err != nil {
					return nil, err
				}
				if err := interrupted(ctx); err != nil {
					return nil, err
				}
				r := modulesResult{DAP: orDash(dap), Modules: []moduleView{}}
				configurable := map[uint32]int{}
				for _, om := range omittedModules(doc, dap) {
					r.Omitted = append(r.Omitted, omittedModuleView{ID: om.ID, IdentNumber: om.IdentNumber, Reason: om.Err.Error()})
				}
				if c, cm, cerr := configure(args[0], doc, dap, nil, nil); cerr == nil {
					// The default configuration: its catalog has the
					// modules with configurable submodules too, with their
					// defaults; the modules it cannot place are omitted.
					m, r.Omitted = cm, nil
					seen := map[uint32]bool{}
					for _, mt := range m.ModuleTypes() {
						seen[mt.IdentNumber] = true
					}
					for _, mo := range c.Modules() {
						id := mo.Module.IdentNumber
						configurable[id] = max(configurable[id], len(mo.Submodules))
						if !seen[id] {
							seen[id] = true
							r.Omitted = append(r.Omitted, omittedModuleView{ID: mo.ID, IdentNumber: id, Reason: optionError(mo)})
						}
					}
				}
				for _, mt := range m.ModuleTypes() {
					v := moduleView{Name: mt.Name, IdentNumber: mt.IdentNumber, Submodules: len(mt.Submodules), Info: mt.Info, Configurable: configurable[mt.IdentNumber]}
					for _, s := range mt.Submodules {
						v.InputLen += int(s.InputLen)
						v.OutputLen += int(s.OutputLen)
					}
					for _, sl := range m.Slots {
						if sl.Module.IdentNumber == mt.IdentNumber {
							v.Slots = append(v.Slots, sl.Number)
						}
					}
					r.Modules = append(r.Modules, v)
				}
				return r, nil
			})
		},
	}
	modulesCmd.Flags().StringVar(&dap, "dap", "", "Device access point ID (default: the first)")
	var subDAP, module string
	submodulesCmd := &cobra.Command{
		Use:   "submodules FILE",
		Short: "List the submodules of the DAP and of the module types, with their IO data and records",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var want uint32
			if module != "" {
				v, err := gsdml.ParseUint32(module)
				if err != nil {
					return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--module %q: %v", module, err), err)
				}
				want = v
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				m, err := loadModel(args[0], subDAP, "")
				if err != nil {
					return nil, err
				}
				if err := interrupted(ctx); err != nil {
					return nil, err
				}
				r := submodulesResult{DAP: orDash(subDAP), Submodules: []submoduleView{}}
				for _, mt := range append([]model.Module{m.DAP}, m.ModuleTypes()...) {
					if module != "" && mt.IdentNumber != want {
						continue
					}
					for _, s := range mt.Submodules {
						r.Submodules = append(r.Submodules, submoduleView{Module: mt.Name, ModuleIdent: mt.IdentNumber, Name: s.Name, IdentNumber: s.IdentNumber,
							API: s.API, Subslot: s.Subslot, InputLen: s.InputLen, OutputLen: s.OutputLen, Records: recordViews(s.Records)})
					}
				}
				if module != "" && len(r.Submodules) == 0 {
					return nil, errs.New(errs.KindNotFound, errs.CodeInvalidInput, fmt.Sprintf("no module with ident number 0x%08X", want), nil)
				}
				return r, nil
			})
		},
	}
	submodulesCmd.Flags().StringVar(&subDAP, "dap", "", "Device access point ID (default: the first)")
	submodulesCmd.Flags().StringVar(&module, "module", "", "Only the submodules of the module with this ident number")
	var modelFile, out, version string
	var supervisor bool
	generateCmd := &cobra.Command{
		Use:   "generate --model MODEL.json [-O FILE|DIR]",
		Short: "Generate the GSDML file of a device model",
		Long: `Generate the GSDML file of a device described by a go-profinet model in JSON
(package model: identity, capabilities, DAP, slots and catalog). Without -O the
file goes to stdout; with a directory it gets its conventional name
(GSDML-V2.35-<vendor>-<family>-<date>.xml).`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			m, err := loadModel("", "", modelFile)
			if err != nil {
				return err
			}
			doc, err := gsdml.FromModel(m, gsdml.GenerateOptions{SupervisorAR: supervisor, PNIOVersion: version})
			if err != nil {
				return errs.InvalidInput(errs.CodeInvalidModel, err.Error(), err)
			}
			b, err := gsdml.Marshal(doc)
			if err != nil {
				return errs.Internal(err)
			}
			if err := interrupted(c.Context()); err != nil {
				return err
			}
			if out == "" {
				if _, err := c.OutOrStdout().Write(b); err != nil {
					return errs.Output(errs.CodeOutputWriteFailed, err)
				}
				return nil
			}
			path := out
			if fi, err := os.Stat(out); err == nil && fi.IsDir() {
				path = filepath.Join(out, gsdml.FileName(m, version, time.Now()))
			}
			if err := writeOutputFile(path, b); err != nil {
				return errs.Output(errs.CodeOutputFileWriteFailed, err)
			}
			return a.result(c, func(context.Context) (any, error) { return generateResult{File: path, Bytes: len(b)}, nil })
		},
	}
	generateCmd.Flags().StringVar(&modelFile, "model", "", "Device model in JSON")
	generateCmd.Flags().StringVarP(&out, "output-file", "O", "", "Write to this file, or under the conventional name to this directory")
	generateCmd.Flags().StringVar(&version, "pnio-version", "", "Declared PNIO_Version (default V2.35)")
	generateCmd.Flags().BoolVar(&supervisor, "device-access", true, "Declare DeviceAccessSupported (supervisor AR)")
	g.AddCommand(inspectCmd, validateCmd, newGSDMLCheckCmd(a), newGSDMLSchemaCmd(a), newGSDXCmd(a), modulesCmd, submodulesCmd, newGSDMLConfigCmd(a), newGSDMLPlanCmd(a), generateCmd)
	return g
}

// optionError is why the default configuration leaves a module out of its
// catalog: its own error, else that of its first configurable submodule
// that has one, else two submodules used in one subslot by default (their
// Defaults), which --plug-submodule decides.
func optionError(mo gsdml.ModuleOption) string {
	if mo.Error != "" {
		return mo.Error
	}
	reason := "not in the catalog of the default configuration"
	by := map[uint16]string{}
	for _, o := range mo.Submodules {
		if o.Error != "" {
			return "configurable submodule " + o.ID + ": " + o.Error
		}
		for _, s := range o.Defaults {
			if other, dup := by[s]; dup && other != o.ID {
				reason = fmt.Sprintf("configurable submodules %s and %s are both used in subslot %d by default: --plug with --plug-submodule chooses one", other, o.ID, s)
			}
			by[s] = o.ID
		}
	}
	return reason
}
