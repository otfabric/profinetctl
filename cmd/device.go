// SPDX-License-Identifier: MIT

package cmd

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/otfabric/go-profinet/controller"
	"github.com/otfabric/go-profinet/pnio"
	"github.com/otfabric/go-profinet/rpc"
)

// slotView is a module of the real configuration.
type slotView struct {
	API         uint32        `json:"api"`
	Slot        uint16        `json:"slot"`
	IdentNumber uint32        `json:"module_ident_number"`
	Submodules  []subslotView `json:"submodules"`
}

type subslotView struct {
	Subslot     uint16 `json:"subslot"`
	IdentNumber uint32 `json:"submodule_ident_number"`
}

// deviceInfoResult is the identity and real configuration of a device.
type deviceInfoResult struct {
	Device deviceView `json:"device"`
	IM0    *im0V      `json:"im0,omitempty"`
	// Modules is the real configuration (RealIdentificationData, 0xF000,
	// of every API APIData, 0xF821, lists).
	Modules []slotView `json:"modules"`
	// APIDiscovery is "apidata" when the device listed its APIs and
	// "unavailable" when it did not, so that Modules holds API 0 only.
	APIDiscovery string   `json:"api_discovery"`
	Errors       []string `json:"errors,omitempty"`
}

// Partial reports parts that could not be read.
func (r deviceInfoResult) Partial() bool { return len(r.Errors) > 0 }

func (r deviceInfoResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	id, _ := identifyResult{Device: r.Device}.MarshalTextOutput()
	b.WriteString(id)
	if m := r.IM0; m != nil {
		fmt.Fprintf(&b, "Order ID:         %s\n", m.OrderID)
		fmt.Fprintf(&b, "Serial number:    %s\n", m.SerialNumber)
		fmt.Fprintf(&b, "Hardware/software: %d / %s\n", m.HardwareRevision, m.SoftwareRevision)
		fmt.Fprintf(&b, "I&M supported:    I&M0 %s\n", strings.Join(m.Supported, " "))
	}
	if len(r.Modules) > 0 {
		b.WriteString("Real configuration:\n")
		for _, s := range r.Modules {
			if s.API != 0 {
				fmt.Fprintf(&b, "  slot %-3d module 0x%08X API 0x%08X\n", s.Slot, s.IdentNumber, s.API)
			} else {
				fmt.Fprintf(&b, "  slot %-3d module 0x%08X\n", s.Slot, s.IdentNumber)
			}
			for _, sub := range s.Submodules {
				fmt.Fprintf(&b, "    subslot 0x%04X submodule 0x%08X\n", sub.Subslot, sub.IdentNumber)
			}
		}
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "error: %s\n", e)
	}
	return b.String(), nil
}

// otherAPIs reports whether a module of an API other than 0 is in the
// real configuration: the table then has an API column.
func (r deviceInfoResult) otherAPIs() bool {
	return slices.ContainsFunc(r.Modules, func(s slotView) bool { return s.API != 0 })
}

func (r deviceInfoResult) TableHeaders() []string {
	h := []string{"SLOT", "MODULE", "SUBSLOT", "SUBMODULE"}
	if r.otherAPIs() {
		h = append([]string{"API"}, h...)
	}
	return h
}

func (r deviceInfoResult) TableRows() [][]string {
	var rows [][]string
	api := r.otherAPIs()
	for _, s := range r.Modules {
		for _, sub := range s.Submodules {
			row := []string{fmt.Sprint(s.Slot), fmt.Sprintf("0x%08X", s.IdentNumber), fmt.Sprintf("0x%04X", sub.Subslot), fmt.Sprintf("0x%08X", sub.IdentNumber)}
			if api {
				row = append([]string{fmt.Sprintf("0x%08X", s.API)}, row...)
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// identReader reads the records of the real configuration;
// *controller.RecordClient is one.
type identReader interface {
	ReadAPIData(ctx context.Context, ar rpc.UUID) ([]uint32, error)
	Read(ctx context.Context, ar rpc.UUID, rec controller.Record, maxLen uint32) ([]byte, error)
}

// maxAPIs bounds the APIs device info reads the real configuration of.
const maxAPIs = 64

// API discovery outcomes of device info (JSON api_discovery).
const (
	// discoveryAPIData: the device listed its APIs (APIData, 0xF821).
	discoveryAPIData = "apidata"
	// discoveryUnavailable: APIData could not be read; API 0 alone was.
	discoveryUnavailable = "unavailable"
)

// realIdent reads the real configuration of a device: its APIs from
// APIData (0xF821), then RealIdentificationData for one API (0xF000) of
// each, outside an AR. A device that does not answer APIData is read at
// API 0 only, and an error says the result may be incomplete. The modules
// are in slot order, API order within a slot.
func realIdent(ctx context.Context, rc identReader, timeout func(context.Context) (context.Context, context.CancelFunc)) (mods []slotView, discovery string, errs []string) {
	cctx, cancel := timeout(ctx)
	apis, err := rc.ReadAPIData(cctx, rpc.NilUUID)
	cancel()
	discovery = discoveryAPIData
	switch {
	case err != nil:
		discovery = discoveryUnavailable
		errs = append(errs, "APIData: "+errorOf(err)+": API discovery unavailable; results may be incomplete (API 0 only)")
		apis = []uint32{0}
	case len(apis) == 0:
		errs = append(errs, "APIData lists no API; API 0 read")
		apis = []uint32{0}
	}
	apis = slices.Compact(slices.Sorted(slices.Values(apis)))
	if len(apis) > maxAPIs {
		errs = append(errs, fmt.Sprintf("APIData lists %d APIs; the first %d read", len(apis), maxAPIs))
		apis = apis[:maxAPIs]
	}
	mods = []slotView{}
	for _, api := range apis {
		got, err := realIdentAPI(ctx, rc, timeout, api)
		if err != nil {
			what := "RealIdentificationData"
			if api != 0 {
				what = fmt.Sprintf("RealIdentificationData (API 0x%08X)", api)
			}
			errs = append(errs, what+": "+errorOf(err))
			continue
		}
		mods = append(mods, got...)
	}
	slices.SortStableFunc(mods, func(a, b slotView) int {
		if c := cmp.Compare(a.Slot, b.Slot); c != 0 {
			return c
		}
		return cmp.Compare(a.API, b.API)
	})
	return mods, discovery, errs
}

// realIdentAPI reads RealIdentificationData for one API: the read header
// addresses the API at slot 0, subslot 1. Entries of another API than the
// one asked for are left out.
func realIdentAPI(ctx context.Context, rc identReader, timeout func(context.Context) (context.Context, context.CancelFunc), api uint32) ([]slotView, error) {
	cctx, cancel := timeout(ctx)
	defer cancel()
	b, err := rc.Read(cctx, rpc.NilUUID, controller.Record{API: api, Slot: 0, Subslot: 1, Index: pnio.IndexRealIdentAPI}, 0)
	if err != nil {
		return nil, err
	}
	blk, _, err := pnio.ReadBlock(b)
	if err != nil {
		return nil, err
	}
	id, err := pnio.DecodeIdentificationData(blk)
	if err != nil {
		return nil, err
	}
	var out []slotView
	for _, a := range id.APIs {
		if a.API != api {
			continue
		}
		for _, sl := range a.Slots {
			v := slotView{API: a.API, Slot: sl.Slot, IdentNumber: sl.IdentNumber, Submodules: []subslotView{}}
			for _, sub := range sl.Submodules {
				v.Submodules = append(v.Submodules, subslotView{Subslot: sub.Subslot, IdentNumber: sub.IdentNumber})
			}
			out = append(out, v)
		}
	}
	return out, nil
}

func newDeviceCmd(a *app) *cobra.Command {
	g := groupCmd("device", "Information about one device")
	var t target
	info := &cobra.Command{
		Use:   "info",
		Short: "Show the identity, I&M0 and real configuration (modules and submodules) of a device",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := t.validate(); err != nil {
				return err
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.openRPC(ctx, &t)
				if err != nil {
					return nil, err
				}
				defer s.Close()
				r := deviceInfoResult{Device: viewOf(s.id), Modules: []slotView{}}
				if e := readIM(ctx, s, 0, 1, 0); e.IM0 != nil {
					r.IM0 = e.IM0
				} else {
					r.Errors = append(r.Errors, "I&M0: "+e.Error)
				}
				mods, discovery, errs := realIdent(ctx, s.rc, s.ctx)
				r.Modules, r.APIDiscovery = mods, discovery
				r.Errors = append(r.Errors, errs...)
				return r, nil
			})
		},
	}
	addRPCTargetFlags(info, &t)
	g.AddCommand(info)
	return g
}

// statusResult is the health of a device.
type statusResult struct {
	Device deviceView `json:"device"`
	// DCP and RPC report whether the device answered each.
	DCP bool `json:"dcp"`
	RPC bool `json:"rpc"`
	// Diagnoses counts the diagnosis entries by severity.
	Diagnoses map[string]int `json:"diagnoses"`
	// Ports reports the link and neighbor of each port.
	Ports  []portStatus `json:"ports"`
	Errors []string     `json:"errors,omitempty"`
}

type portStatus struct {
	Subslot uint16 `json:"subslot"`
	PortID  string `json:"port_id"`
	Link    string `json:"link"`
	Peer    string `json:"peer,omitempty"`
}

func (r statusResult) Partial() bool { return len(r.Errors) > 0 }

func (r statusResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	d := r.Device
	fmt.Fprintf(&b, "%s (%s) %s\n", orDash(d.NameOfStation), d.MAC, d.IP)
	fmt.Fprintf(&b, "DCP: %s  RPC: %s\n", okString(r.DCP), okString(r.RPC))
	total := 0
	var parts []string
	for _, k := range []string{"fault", "maintenance required", "maintenance demanded", "qualified"} {
		if n := r.Diagnoses[k]; n > 0 {
			total += n
			parts = append(parts, fmt.Sprintf("%d %s", n, k))
		}
	}
	if total == 0 {
		b.WriteString("Diagnosis: none\n")
	} else {
		fmt.Fprintf(&b, "Diagnosis: %s\n", strings.Join(parts, ", "))
	}
	for _, p := range r.Ports {
		fmt.Fprintf(&b, "Port 0x%04X %s: link %s, peer %s\n", p.Subslot, p.PortID, p.Link, orDash(p.Peer))
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "error: %s\n", e)
	}
	return b.String(), nil
}

func okString(ok bool) string {
	if ok {
		return "ok"
	}
	return "no answer"
}

func (r statusResult) TableHeaders() []string {
	return []string{"STATION", "MAC", "IP", "DCP", "RPC", "DIAGNOSES", "PORTS"}
}

func (r statusResult) TableRows() [][]string {
	n := 0
	for _, v := range r.Diagnoses {
		n += v
	}
	var ports []string
	for _, p := range r.Ports {
		ports = append(ports, p.PortID+":"+p.Link)
	}
	return [][]string{{orDash(r.Device.NameOfStation), r.Device.MAC, r.Device.IP, okString(r.DCP), okString(r.RPC), fmt.Sprint(n), orDash(strings.Join(ports, " "))}}
}

func linkName(s uint16) string {
	switch s & 0xFF {
	case pnio.LinkStateUp:
		return "up"
	case pnio.LinkStateDown:
		return "down"
	default:
		return fmt.Sprintf("state %d", s&0xFF)
	}
}

func newStatusCmd(a *app) *cobra.Command {
	var t target
	c := &cobra.Command{
		Use:   "status",
		Short: "Show the health of a device: reachability, diagnosis, port links",
		Long: `Check that a device answers DCP and DCE/RPC, count its diagnosis entries by
severity and report the link and neighbor of its ports (PDRealData). Exit code
7 when a part could not be read.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := t.validate(); err != nil {
				return err
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.openRPC(ctx, &t)
				if err != nil {
					return nil, err
				}
				defer s.Close()
				r := statusResult{Device: viewOf(s.id), DCP: true, Diagnoses: map[string]int{}, Ports: []portStatus{}}
				cctx, cancel := s.ctx(ctx)
				d, err := s.rc.ReadDiagnosis(cctx, rpc.NilUUID, 0, 0, false)
				cancel()
				if err != nil {
					r.Errors = append(r.Errors, "diagnosis: "+errorOf(err))
				} else {
					r.RPC = true
					for _, v := range diagViews(d) {
						r.Diagnoses[v.Severity]++
					}
				}
				ports, err := readPorts(ctx, s)
				if err != nil {
					r.Errors = append(r.Errors, "PDRealData: "+errorOf(err))
				} else {
					r.RPC = true
					for _, p := range ports {
						ps := portStatus{Subslot: p.Subslot, PortID: p.OwnPortID, Link: linkName(p.LinkState)}
						if len(p.Peers) > 0 {
							var peers []string
							for _, peer := range p.Peers {
								peers = append(peers, peerStation(peer)+" "+peerPort(peer))
							}
							ps.Peer = strings.Join(peers, ", ")
						}
						r.Ports = append(r.Ports, ps)
					}
				}
				return r, nil
			})
		},
	}
	addRPCTargetFlags(c, &t)
	return c
}

// readPorts reads the port data of a device (PDRealData).
func readPorts(ctx context.Context, s *rpcSession) ([]pnio.PDPortDataReal, error) {
	cctx, cancel := s.ctx(ctx)
	defer cancel()
	b, err := s.rc.Read(cctx, rpc.NilUUID, controller.Record{Slot: 0, Subslot: 1, Index: pnio.IndexPDRealData}, 0)
	if err != nil {
		return nil, err
	}
	return pnio.DecodePDRealData(b)
}
