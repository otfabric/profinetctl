// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/otfabric/go-profinet/controller"
	"github.com/otfabric/go-profinet/gsdml"
	"github.com/otfabric/go-profinet/gsdml/gsdx"
	"github.com/otfabric/go-profinet/model"
	"github.com/otfabric/go-profinet/mrp"
	"github.com/otfabric/go-profinet/pnio"
	"github.com/otfabric/go-profinet/rpc"
	"github.com/otfabric/profinetctl/internal/errs"
)

// gsdxPart is a GSDML part of a GSDX container and its check.
type gsdxPart struct {
	Name        string `json:"name"`
	Errors      int    `json:"errors"`
	Warnings    int    `json:"warnings"`
	Unvalidated int    `json:"unvalidated"`
	Error       string `json:"error,omitempty"`
}

// gsdxResult is what gsdml gsdx reports.
type gsdxResult struct {
	File    string        `json:"file"`
	Package *gsdx.Package `json:"package"`
	GSDML   []gsdxPart    `json:"gsdml"`
	Scope   string        `json:"scope"`
}

const gsdxScope = "the public Open Packaging Conventions layer (ISO/IEC 29500-2): parts, content types, relationships, signature metadata; signatures are not verified and PI's GSDX rules (members only) are not applied"

func (r gsdxResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	p := r.Package
	fmt.Fprintf(&b, "%s: %d part(s), %d signature(s), %d issue(s)\n", r.File, len(p.Parts), len(p.Signatures), len(p.Issues))
	for _, pt := range p.Parts {
		fmt.Fprintf(&b, "  %-48s %-40s %8d\n", pt.Name, pt.ContentType, pt.UncompressedSize)
	}
	for _, s := range p.Signatures {
		subject := ""
		if len(s.Certificates) > 0 {
			subject = s.Certificates[0].Subject
		}
		fmt.Fprintf(&b, "signature %s: %d part reference(s), signed by %q, verified: no (%s)\n", s.Part, len(s.PartReferences), subject, s.Reason)
	}
	for _, g := range r.GSDML {
		if g.Error != "" {
			fmt.Fprintf(&b, "GSDML %s: %s\n", g.Name, g.Error)
			continue
		}
		fmt.Fprintf(&b, "GSDML %s: %d error(s), %d warning(s), %d unvalidated\n", g.Name, g.Errors, g.Warnings, g.Unvalidated)
	}
	for _, i := range p.Issues {
		fmt.Fprintf(&b, "issue: %s\n", i)
	}
	fmt.Fprintf(&b, "inspected: %s\n", r.Scope)
	return b.String(), nil
}

// Partial reports a container with issues or GSDML errors: exit code 7.
func (r gsdxResult) Partial() bool {
	if len(r.Package.Issues) > 0 {
		return true
	}
	for _, g := range r.GSDML {
		if g.Errors > 0 || g.Error != "" {
			return true
		}
	}
	return false
}

func newGSDXCmd(a *app) *cobra.Command {
	var extract, out string
	c := &cobra.Command{
		Use:   "gsdx FILE",
		Short: "Inspect a GSDX container: parts, relationships, signature metadata, its GSDML files checked",
		Long: `Read a GSDX file (an Open Packaging Conventions container) safely: size and
compression limits, hostile part names refused. Lists its parts with their
content types, relationships, core properties and the metadata of its digital
signatures (not verified), and runs the GSDML check on every GSDML part.
--extract writes one part to -O FILE (or stdout). PI's GSDX rules are members
only and are not applied. Exit code 7 when the container or a GSDML part has
issues.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if extract != "" {
				p, err := gsdx.OpenFile(args[0])
				if err != nil {
					return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("%s: %v", args[0], err), err)
				}
				b, err := p.ReadPart(extract)
				if err != nil {
					return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--extract %s: %v", extract, err), err)
				}
				if out == "" {
					if _, err := c.OutOrStdout().Write(b); err != nil {
						return errs.Output(errs.CodeOutputWriteFailed, err)
					}
					return nil
				}
				if err := writeOutputFile(out, b); err != nil {
					return errs.Output(errs.CodeOutputFileWriteFailed, err)
				}
				return nil
			}
			return a.result(c, func(context.Context) (any, error) {
				p, err := gsdx.OpenFile(args[0])
				if err != nil {
					return nil, errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("%s: %v", args[0], err), err)
				}
				r := gsdxResult{File: args[0], Package: p, GSDML: []gsdxPart{}, Scope: gsdxScope}
				for _, name := range p.GSDMLParts() {
					g := gsdxPart{Name: name}
					if _, rep, err := p.CheckGSDML(name); err != nil {
						g.Error = err.Error()
					} else {
						g.Errors, g.Warnings, g.Unvalidated = rep.Count(gsdml.SeverityError), rep.Count(gsdml.SeverityWarning), rep.Count(gsdml.SeverityUnvalidated)
					}
					r.GSDML = append(r.GSDML, g)
				}
				return r, nil
			})
		},
	}
	c.Flags().StringVar(&extract, "extract", "", "Write this part (e.g. /GSDML-...xml) instead of the report")
	c.Flags().StringVarP(&out, "output-file", "O", "", "With --extract: the file to write (default: stdout)")
	return c
}

// mrpResult is the MRP state a device reports.
type mrpResult struct {
	Station    string        `json:"station"`
	MAC        string        `json:"mac"`
	Role       string        `json:"role"`
	DomainName string        `json:"domain_name"`
	Domain     string        `json:"domain_uuid"`
	RingState  string        `json:"ring_state,omitempty"`
	RingPorts  []mrpPortView `json:"ring_ports"`
	// Errors lists the port submodules whose record could not be read.
	Errors []string `json:"errors,omitempty"`
}

// Partial reports ring port records that could not be read.
func (r mrpResult) Partial() bool { return len(r.Errors) > 0 }

type mrpPortView struct {
	Subslot string `json:"subslot"`
	Domain  string `json:"domain_uuid"`
}

func (r mrpResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s): MRP %s, domain %q %s\n", r.Station, r.MAC, r.Role, r.DomainName, r.Domain)
	if r.RingState != "" {
		fmt.Fprintf(&b, "ring %s\n", r.RingState)
	}
	for _, p := range r.RingPorts {
		fmt.Fprintf(&b, "ring port %s\n", p.Subslot)
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "error: port %s\n", e)
	}
	return b.String(), nil
}

func roleName(r uint16) string {
	switch r {
	case pnio.MrpRoleClient:
		return "client"
	case pnio.MrpRoleManager:
		return "manager"
	default:
		return "off"
	}
}

func uuidString(d [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", d[0:4], d[4:6], d[6:8], d[8:10], d[10:16])
}

func newMRPCmd(a *app) *cobra.Command {
	g := groupCmd("mrp", "Media redundancy (MRP) of a device")
	var t target
	var ports uint8
	status := &cobra.Command{
		Use:   "status",
		Short: "Read a device's MRP role, domain, ring state and ring ports (PDInterfaceMrpDataReal, PDPortMrpDataReal)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := t.validate(); err != nil {
				return err
			}
			if ports == 0 {
				return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--ports 0: want 1 to %d", maxPorts), nil)
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.openRPC(ctx, &t)
				if err != nil {
					return nil, err
				}
				defer s.Close()
				read := func(sub uint16, idx pnio.Index) (pnio.RawBlock, bool, error) {
					// Each read has its own --timeout.
					cctx, cancel := s.ctx(ctx)
					defer cancel()
					b, err := s.rc.Read(cctx, rpc.NilUUID, controller.Record{Slot: 0, Subslot: sub, Index: idx}, 4096)
					var se *pnio.StatusError
					if errors.As(err, &se) && se.Status.Decode == pnio.DecodePNIORW && (se.Status.Code1 == pnio.RWInvalidIndex || se.Status.Code1 == pnio.RWInvalidSlot) {
						// A device without media redundancy has no such
						// record; one with fewer ports no such submodule.
						return pnio.RawBlock{}, false, nil
					}
					if err != nil {
						return pnio.RawBlock{}, false, classify(fmt.Sprintf("read %#x of %#x", uint16(idx), sub), err)
					}
					if len(b) == 0 {
						return pnio.RawBlock{}, false, nil
					}
					blk, _, err := pnio.ReadBlock(b)
					return blk, err == nil, err
				}
				blk, ok, err := read(model.SubslotInterface, pnio.IndexPDInterfaceMrpDataReal)
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, errs.New(errs.KindUnsupported, errs.CodeUnsupported, "the device has no media redundancy (no PDInterfaceMrpDataReal record)", nil)
				}
				real, err := pnio.DecodePDInterfaceMrpDataReal(blk)
				if err != nil {
					return nil, classify("PDInterfaceMrpDataReal", err)
				}
				r := mrpResult{Station: s.id.NameOfStation, MAC: s.id.MAC.String(), Role: roleName(real.Role), DomainName: real.DomainName,
					Domain: uuidString(real.Domain), RingPorts: []mrpPortView{}}
				if real.HasRingState {
					r.RingState = map[bool]string{true: "closed", false: "open"}[real.RingState == uint16(mrp.RingClosed)]
				}
				r.RingPorts, r.Errors = ringPorts(ports, func(sub uint16) (pnio.RawBlock, bool, error) {
					return read(sub, pnio.IndexPDPortMrpDataReal)
				})
				return r, nil
			})
		},
	}
	addRPCTargetFlags(status, &t)
	status.Flags().Uint8Var(&ports, "ports", 4, fmt.Sprintf("How many port submodules to read (1 to %d)", maxPorts))
	g.AddCommand(status)
	return g
}

// ringPorts reads PDPortMrpDataReal of the first n port submodules: the
// ring ports, and the ports whose record could not be read or decoded. A
// port submodule the device does not have (ok false) is left out.
func ringPorts(n uint8, read func(sub uint16) (pnio.RawBlock, bool, error)) ([]mrpPortView, []string) {
	ports := []mrpPortView{}
	var failed []string
	for sub := model.SubslotPort1; sub < model.SubslotPort1+uint16(n); sub++ {
		blk, ok, err := read(sub)
		if err == nil && !ok {
			continue
		}
		var pr pnio.PDPortMrpData
		if err == nil {
			pr, err = pnio.DecodePDPortMrpData(blk)
			if err != nil {
				err = classify("PDPortMrpDataReal", err)
			}
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%#04x: %v", sub, err))
			continue
		}
		ports = append(ports, mrpPortView{Subslot: fmt.Sprintf("%#04x", sub), Domain: uuidString(pr.Domain)})
	}
	return ports, failed
}

// maxPorts is the number of port submodules of an interface: 0x8001 to
// 0x80FF, as --peer-check numbers them.
const maxPorts = 255

// mrpSetting reads --mrp-role.
func mrpSetting(role, name string) (*controller.MRPSetting, error) {
	if len(name) > maxNameLen {
		return nil, fmt.Errorf("--mrp-domain-name of %d octets (at most %d)", len(name), maxNameLen)
	}
	switch role {
	case "":
		if name != "" {
			return nil, errors.New("--mrp-domain-name needs --mrp-role")
		}
		return nil, nil
	case "client":
		return &controller.MRPSetting{Role: mrp.RoleClient, DomainName: name}, nil
	case "manager":
		return &controller.MRPSetting{Role: mrp.RoleManager, DomainName: name}, nil
	case "off":
		return &controller.MRPSetting{Role: mrp.RoleDisabled, DomainName: name}, nil
	default:
		return nil, fmt.Errorf("%q: want client, manager or off", role)
	}
}
