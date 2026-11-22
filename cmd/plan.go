// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/otfabric/go-profinet"
	"github.com/otfabric/go-profinet/controller"
	"github.com/otfabric/go-profinet/gsdml"
	"github.com/otfabric/go-profinet/model"
	"github.com/otfabric/go-profinet/pnio"
	"github.com/otfabric/profinetctl/internal/errs"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// addPlanFlags adds the flags of a parameter plan: gsdml plan and the
// commands that establish an AR.
func addPlanFlags(f *pflag.FlagSet, values *[]string, defaults *bool) {
	f.StringArrayVar(values, "param-value", nil, "Set a field of a parameter record from the GSDML file: SLOT:SUBSLOT:INDEX:FIELD=VALUE, FIELD the field's ID or name (repeatable; needs --gsdml)")
	f.BoolVar(defaults, "param-defaults", false, "Write every parameter record of the plugged submodules, with the GSDML file's defaults where --param-value sets none (needs --gsdml)")
}

// maxParamValues bounds the --param-value flags of one invocation.
const maxParamValues = 4096

// parseParamValues reads --param-value flags, SLOT:SUBSLOT:INDEX:FIELD=VALUE,
// into the value sets of go-profinet's PlanParameters, one per record in
// the order the records are first named. FIELD is the field's ID or a name
// only one field of the record has (gsdml.ParameterRecord.Field); the value
// is what follows the first '=' after it, so it may contain '=' and ':'.
// A field given twice for one record is refused, not overwritten.
func parseParamValues(vs []string) ([]gsdml.ParameterValues, error) {
	bad := func(v string, format string, args ...any) error {
		msg := fmt.Sprintf("--param-value %q: ", clip(v)) + fmt.Sprintf(format, args...)
		return errs.InvalidInput(errs.CodeInvalidInput, msg, nil)
	}
	if len(vs) > maxParamValues {
		return nil, errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("more than %d --param-value flags", maxParamValues), nil)
	}
	type key struct{ slot, subslot, index uint16 }
	at := map[key]int{}
	var out []gsdml.ParameterValues
	for _, v := range vs {
		f := strings.SplitN(v, ":", 4)
		if len(f) != 4 {
			return nil, bad(v, "want SLOT:SUBSLOT:INDEX:FIELD=VALUE")
		}
		slot, e1 := parseUint16(f[0])
		sub, e2 := parseUint16(f[1])
		idx, e3 := parseUint16(f[2])
		if err := errors.Join(e1, e2, e3); err != nil {
			return nil, bad(v, "%v", err)
		}
		field, value, ok := strings.Cut(f[3], "=")
		if !ok || field == "" {
			return nil, bad(v, "want FIELD=VALUE after the index (the field's ID or name)")
		}
		k := key{slot, sub, idx}
		i, seen := at[k]
		if !seen {
			i = len(out)
			at[k] = i
			out = append(out, gsdml.ParameterValues{Slot: slot, Subslot: sub, Index: pnio.Index(idx), Values: map[string]string{}})
		}
		if _, dup := out[i].Values[field]; dup {
			return nil, bad(v, "field %q of slot %d subslot %d record %d given twice", clip(field), slot, sub, idx)
		}
		out[i].Values[field] = value
	}
	return out, nil
}

// clip shortens a flag value for a message.
func clip(s string) string {
	const n = 80
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// planError is the typed error of a plan go-profinet refused: nothing was
// sent. A file go-profinet does not support is unsupported_gsdml, a file
// that does not parse as the plan needs invalid_gsdml, and values or a
// configuration that do not match the file invalid_input.
func planError(err error) error {
	msg := "parameter plan: " + err.Error()
	switch {
	case errors.Is(err, profinet.ErrUnsupported):
		return errs.InvalidInput(errs.CodeUnsupportedGSDML, msg, err)
	case errors.Is(err, profinet.ErrMalformed):
		return errs.InvalidInput(errs.CodeInvalidGSDML, msg, err)
	default:
		return errs.InvalidInput(errs.CodeInvalidInput, msg, err)
	}
}

// planOf builds the parameter plan of a plugged model m of doc.
// It never returns a nil plan: a refused one holds its issues.
func planOf(doc *gsdml.Document, m *model.Device, dap string, values []string, defaults bool) (gsdml.ParameterPlan, error) {
	vs, err := parseParamValues(values)
	if err != nil {
		return gsdml.ParameterPlan{}, err
	}
	plan, err := gsdml.PlanParameters(doc, m, gsdml.PlanOptions{DAP: dap, Defaults: defaults, Values: vs})
	if plan == nil {
		return gsdml.ParameterPlan{}, err
	}
	return *plan, err
}

// planWarnings are the issues of a plan that do not stop it.
func planWarnings(plan gsdml.ParameterPlan) []gsdml.Issue {
	var out []gsdml.Issue
	for _, i := range plan.Issues {
		if i.Severity != gsdml.SeverityError {
			out = append(out, i)
		}
	}
	return out
}

// addPlan puts the writes of a plan before the --param records of an AR,
// in the plan's order. A --param record of the same slot, subslot and index
// as a planned one is refused: the device would get both, the later one
// winning.
func addPlan(cfg *controller.DeviceConfig, plan gsdml.ParameterPlan) error {
	type key struct {
		slot, subslot uint16
		index         pnio.Index
	}
	planned := map[key]bool{}
	var ps []controller.Parameter
	for _, w := range plan.Writes {
		planned[key{w.Write.Slot, w.Write.Subslot, w.Write.Index}] = true
		ps = append(ps, controller.Parameter(w.Write))
	}
	for _, p := range cfg.Parameters {
		if planned[key{p.Slot, p.Subslot, p.Index}] {
			return errs.New(errs.KindUsage, errs.CodeInvalidFlagCombination,
				fmt.Sprintf("--param %d:%d:%d writes a record the parameter plan (--param-value, --param-defaults) writes too: give one", p.Slot, p.Subslot, p.Index), nil)
		}
	}
	cfg.Parameters = append(ps, cfg.Parameters...)
	return nil
}

// planWriteView is a record write of a plan.
type planWriteView struct {
	Slot             uint16 `json:"slot"`
	Subslot          uint16 `json:"subslot"`
	Index            uint16 `json:"index"`
	Name             string `json:"name,omitempty"`
	ModuleIdent      uint32 `json:"module_ident_number"`
	SubmoduleIdent   uint32 `json:"submodule_ident_number"`
	TransferSequence uint16 `json:"transfer_sequence"`
	Values           int    `json:"values"`
	Length           int    `json:"length"`
	Data             string `json:"data"`
}

// planResult is what gsdml plan reports.
type planResult struct {
	File string `json:"file"`
	DAP  string `json:"dap"`
	// Planned is false when go-profinet refused the plan for the errors in
	// Issues: Writes is then empty.
	Planned     bool            `json:"planned"`
	Writes      []planWriteView `json:"writes"`
	Errors      int             `json:"errors"`
	Warnings    int             `json:"warnings"`
	Unvalidated int             `json:"unvalidated"`
	Issues      []gsdml.Issue   `json:"issues"`
}

func newPlanResult(file, dap string, plan gsdml.ParameterPlan, planned bool) planResult {
	r := planResult{File: file, DAP: orDash(dap), Planned: planned, Writes: []planWriteView{}, Issues: []gsdml.Issue{}}
	for _, w := range plan.Writes {
		r.Writes = append(r.Writes, planWriteView{Slot: w.Write.Slot, Subslot: w.Write.Subslot, Index: uint16(w.Write.Index), Name: w.Name,
			ModuleIdent: w.ModuleIdent, SubmoduleIdent: w.SubmoduleIdent, TransferSequence: w.TransferSequence, Values: w.Values,
			Length: len(w.Write.Data), Data: hex.EncodeToString(w.Write.Data)})
	}
	for _, i := range plan.Issues {
		switch i.Severity {
		case gsdml.SeverityError:
			r.Errors++
		case gsdml.SeverityWarning:
			r.Warnings++
		default:
			r.Unvalidated++
		}
		r.Issues = append(r.Issues, i)
	}
	return r
}

func (r planResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, w := range r.Writes {
		name := w.Name
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(&b, "slot %d subslot 0x%04X record %d %-24s seq %d  %d octet(s), %d value(s): %s\n",
			w.Slot, w.Subslot, w.Index, name, w.TransferSequence, w.Length, w.Values, w.Data)
	}
	for _, i := range r.Issues {
		fmt.Fprintf(&b, "%s\n", issueText(i))
	}
	if r.Planned {
		fmt.Fprintf(&b, "%d record write(s); %d warning(s), %d unvalidated\n", len(r.Writes), r.Warnings, r.Unvalidated)
	} else {
		fmt.Fprintf(&b, "refused: %d error(s)\n", r.Errors)
	}
	return b.String(), nil
}

func (r planResult) TableHeaders() []string {
	return []string{"SLOT", "SUBSLOT", "INDEX", "NAME", "SEQ", "VALUES", "DATA"}
}

func (r planResult) TableRows() [][]string {
	var rows [][]string
	for _, w := range r.Writes {
		rows = append(rows, []string{fmt.Sprint(w.Slot), fmt.Sprintf("0x%04X", w.Subslot), fmt.Sprint(w.Index), w.Name,
			fmt.Sprint(w.TransferSequence), fmt.Sprint(w.Values), w.Data})
	}
	return rows
}

// Partial reports a plan go-profinet refused for errors of the file or
// the values: exit code 7, with the issues.
func (r planResult) Partial() bool { return !r.Planned }

func newGSDMLPlanCmd(a *app) *cobra.Command {
	var dap string
	var plugs, values []string
	var defaults bool
	c := &cobra.Command{
		Use:   "plan FILE [--plug SLOT=IDENT]... [--param-value SLOT:SUBSLOT:INDEX:FIELD=VALUE]... [--param-defaults]",
		Short: "Show the parameter records an AR would write, from the GSDML file and chosen values; sends nothing",
		Long: `Build the parameter plan of a device configuration with go-profinet's
PlanParameters, and show it without sending anything: the parameter records an
AR with the same --gsdml, --dap, --plug, --param-value and --param-defaults
writes before PrmEnd, in the order it writes them, and the issues of the
records.

--param-value sets a field of a record, by the field's ID or by a name only one
field of the record has, in the lexical form of the GSDML file's DefaultValue;
only fields the file marks Changeable can be set. A record is written when a
value sets one of its fields; --param-defaults writes every record of every
plugged submodule, with its defaults where no value is given. The records of a
submodule go in ascending TransferSequence, then those without one in file
order; submodules in slot and subslot order.

When go-profinet refuses the plan for errors of the records (a value outside
the field's AllowedValues or ValueItem, a record that does not build, a
TransferSequence that is not 1 to N), the errors are listed and the exit code
is 7. Values or a configuration that do not match the file exit 3.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return a.result(c, func(ctx context.Context) (any, error) {
				m, doc, err := loadModelDoc(args[0], dap, "")
				if err != nil {
					return nil, err
				}
				if err := plug(m, plugs, slotRulesOf(doc, dap)); err != nil {
					return nil, err
				}
				if err := m.Validate(); err != nil {
					return nil, errs.InvalidInput(errs.CodeInvalidModel, err.Error(), err)
				}
				if err := interrupted(ctx); err != nil {
					return nil, err
				}
				plan, err := planOf(doc, m, dap, values, defaults)
				if err != nil {
					if _, typed := errs.As(err); typed {
						return nil, err
					}
					if errors.Is(err, profinet.ErrInvalidConfig) && hasErrorIssue(plan) {
						return newPlanResult(args[0], dap, plan, false), nil
					}
					return nil, planError(err)
				}
				return newPlanResult(args[0], dap, plan, true), nil
			})
		},
	}
	f := c.Flags()
	f.StringVar(&dap, "dap", "", "Device access point ID (default: the first)")
	f.StringArrayVar(&plugs, "plug", nil, "Plug the module with this ident number from the GSDML catalog: SLOT=IDENT (repeatable), in a slot the GSDML file allows it in")
	addPlanFlags(f, &values, &defaults)
	return c
}

// hasErrorIssue reports whether a refused plan says why in its issues.
func hasErrorIssue(plan gsdml.ParameterPlan) bool {
	for _, i := range plan.Issues {
		if i.Severity == gsdml.SeverityError {
			return true
		}
	}
	return false
}
