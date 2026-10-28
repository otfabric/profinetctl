// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/otfabric/go-profinet/controller"
	"github.com/otfabric/go-profinet/dcp"
	"github.com/otfabric/go-profinet/gsdml"
	"github.com/otfabric/go-profinet/model"
	"github.com/otfabric/profinetctl/internal/errs"
	"github.com/otfabric/profinetctl/internal/format"
)

// helloEvent is one DCP Hello heard.
type helloEvent struct {
	Time   string     `json:"time"`
	Event  string     `json:"event"`
	Device deviceView `json:"device"`
}

func (e helloEvent) MarshalTextOutput() (string, error) {
	d := e.Device
	return fmt.Sprintf("%s hello %s %s %s/%s\n", e.Time, d.MAC, d.NameOfStation, d.IP, d.Mask), nil
}
func (e helloEvent) TableHeaders() []string { return []string{"TIME", "MAC", "STATION", "IP"} }
func (e helloEvent) TableRows() [][]string {
	return [][]string{{e.Time, e.Device.MAC, e.Device.NameOfStation, e.Device.IP}}
}

func newHelloCmd(a *app) *cobra.Command {
	g := groupCmd("hello", "DCP Hello: devices announcing themselves (Fast Startup)")
	var duration time.Duration
	listen := &cobra.Command{
		Use:   "listen",
		Short: "Stream the DCP Hello frames devices send at start and on link up",
		Long: `Listen for DCP Hello requests (FrameID 0xFEFC to 01-0E-CF-00-00-01): a
device with DeviceInitiative "Hello" announces its name, IP suite and identity
so that a controller can connect without Identify. One event per Hello.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return a.stream(c, func(ctx context.Context, out *format.StreamWriter) error {
				s, err := a.open()
				if err != nil {
					return err
				}
				defer s.Close()
				if duration > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, duration)
					defer cancel()
				}
				for id := range s.dcp.Hellos(ctx, 64) {
					if err := out.Write(helloEvent{Time: stamp(), Event: "hello", Device: viewOf(id)}); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	listen.Flags().DurationVar(&duration, "duration", 0, "How long to listen (0: until interrupted)")
	g.AddCommand(listen)
	return g
}

// checkResult is the full GSDML check report.
type checkResult struct {
	File          string        `json:"file"`
	SchemaVersion string        `json:"schema_version,omitempty"`
	Errors        int           `json:"errors"`
	Warnings      int           `json:"warnings"`
	Unvalidated   int           `json:"unvalidated"`
	Issues        []gsdml.Issue `json:"issues"`
	Scope         string        `json:"scope"`
	strict        bool
}

const checkScope = "go-profinet's GSDML catalog and semantic rules (IO data length, slots, record indexes, ident numbers, subslots, reduction ratios); not PI's GSDML checker"

func (r checkResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, i := range r.Issues {
		rule := i.Rule
		if rule == "" {
			rule = gsdml.RuleStructure
		}
		fmt.Fprintf(&b, "%-11s %-14s %s\n", i.Severity, rule, i)
	}
	fmt.Fprintf(&b, "%s: %d error(s), %d warning(s), %d unvalidated\n", r.File, r.Errors, r.Warnings, r.Unvalidated)
	fmt.Fprintf(&b, "checked: %s\n", r.Scope)
	return b.String(), nil
}

func (r checkResult) TableHeaders() []string { return []string{"SEVERITY", "RULE", "PATH", "ISSUE"} }

func (r checkResult) TableRows() [][]string {
	var rows [][]string
	for _, i := range r.Issues {
		rows = append(rows, []string{i.Severity.String(), i.Rule, i.Path, i.Message})
	}
	return rows
}

// Partial reports errors, or with --strict any issue: exit code 7.
func (r checkResult) Partial() bool { return r.Errors > 0 || (r.strict && len(r.Issues) > 0) }

func newGSDMLCheckCmd(a *app) *cobra.Command {
	var strict bool
	c := &cobra.Command{
		Use:   "check FILE",
		Short: "Check a GSDML file: errors, warnings and unvalidated content, each with its rule",
		Long: `Run go-profinet's full GSDML check: the catalog (structure, types, references)
as validate does, the semantic rules (IO data that does not fit an RT frame is an
error; slots, record indexes, module ident numbers, port subslots and reduction
ratios are warnings, each with its basis), and what was not validated (elements
outside the catalog, newer schema versions). Exit code 7 when there are errors,
or with --strict any issue. This is not PI's GSDML checker.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return a.result(c, func(context.Context) (any, error) {
				doc, err := readGSDML(args[0])
				if err != nil {
					return nil, err
				}
				rep := gsdml.Check(doc)
				r := checkResult{File: args[0], SchemaVersion: rep.SchemaVersion, Issues: rep.Issues, Scope: checkScope, strict: strict,
					Errors: rep.Count(gsdml.SeverityError), Warnings: rep.Count(gsdml.SeverityWarning), Unvalidated: rep.Count(gsdml.SeverityUnvalidated)}
				if r.Issues == nil {
					r.Issues = []gsdml.Issue{}
				}
				return r, nil
			})
		},
	}
	c.Flags().BoolVar(&strict, "strict", false, "Exit code 7 on warnings and unvalidated content too")
	return c
}

// topoCheckResult is the verdict of every expected link.
type topoCheckResult struct {
	OK     bool                      `json:"ok"`
	Report controller.TopologyReport `json:"report"`
}

// MarshalTextOutput writes one line per expected link, then the links
// nobody expected, the stations that could not be read and the verdict.
func (r topoCheckResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, l := range r.Report.Links {
		fmt.Fprintf(&b, "%s — %s: %s", l.Link.A, l.Link.B, l.Verdict)
		if l.Detail != "" {
			fmt.Fprintf(&b, " (%s)", l.Detail)
		}
		b.WriteString("\n")
	}
	for _, l := range r.Report.Unexpected {
		fmt.Fprintf(&b, "%s — %s: unexpected\n", l.A, l.B)
	}
	stations := make([]string, 0, len(r.Report.Unreachable))
	for s := range r.Report.Unreachable {
		stations = append(stations, s)
	}
	sort.Strings(stations)
	for _, s := range stations {
		fmt.Fprintf(&b, "%s: unreachable (%s)\n", s, r.Report.Unreachable[s])
	}
	if r.OK {
		b.WriteString("topology as expected\n")
	} else {
		b.WriteString("topology differs\n")
	}
	return b.String(), nil
}

// Partial reports a topology that differs: exit code 7.
func (r topoCheckResult) Partial() bool { return !r.OK }

// parseLinks reads links given as STATION/PORT=STATION/PORT.
func parseLinks(specs []string) ([]controller.Link, error) {
	var out []controller.Link
	ref := func(s string) (controller.PortRef, bool) {
		st, p, ok := strings.Cut(strings.TrimSpace(s), "/")
		return controller.PortRef{Station: st, Port: p}, ok && st != "" && p != ""
	}
	for _, s := range specs {
		l, r, ok := strings.Cut(s, "=")
		a, okA := ref(l)
		b, okB := ref(r)
		if !ok || !okA || !okB {
			return nil, fmt.Errorf("%q: want STATION/PORT=STATION/PORT", s)
		}
		out = append(out, controller.Link{A: a, B: b})
	}
	return out, nil
}

func newTopologyCheckCmd(a *app) *cobra.Command {
	var links []string
	var file string
	c := &cobra.Command{
		Use:   "check --link STATION/PORT=STATION/PORT ... | --expect FILE",
		Short: "Verify the cabling against the expected links (PDRealData of every station)",
		Long: `Read the port data (PDRealData) of every station the expected links name and
judge each link: ok, missing, wrong peer, unknown port or unverified (a station
that could not be read). --expect reads a JSON list of {"a":{"station","port"},
"b":{"station","port"}}. Exit code 7 when the topology differs.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return a.result(c, func(ctx context.Context) (any, error) {
				expected, err := parseLinks(links)
				if err != nil {
					return nil, errs.InvalidInput(errs.CodeInvalidInput, "--link "+err.Error(), err)
				}
				if file != "" {
					b, err := os.ReadFile(file)
					if err != nil {
						return nil, errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--expect: %v", err), err)
					}
					var more []controller.Link
					if err := json.Unmarshal(b, &more); err != nil {
						return nil, errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--expect %s: %v", file, err), err)
					}
					expected = append(expected, more...)
				}
				if len(expected) == 0 {
					return nil, errs.New(errs.KindUsage, errs.CodeInvalidInput, "no expected link: give --link or --expect", nil)
				}
				s, err := a.open()
				if err != nil {
					return nil, err
				}
				_ = s.dcp.Close()
				defer s.link.Close()
				ctrl, err := controller.New(controller.Config{
					Mux: s.link.Mux, ListenPacket: s.link.ListenPacket, StationName: "profinetctl",
					VendorID: toolVendorID, DeviceID: toolDeviceID, Instance: 1,
					DCP: dcp.ClientOptions{Timeout: a.timeout / 4, Retries: 3}, Logger: a.logger(),
				})
				if err != nil {
					return nil, classify("controller", err)
				}
				defer func() { _ = ctrl.Close() }()
				rep, err := ctrl.CheckTopology(ctx, expected)
				if err != nil {
					return nil, classify("topology check", err)
				}
				sort.SliceStable(rep.Links, func(i, j int) bool { return rep.Links[i].Link.String() < rep.Links[j].Link.String() })
				return topoCheckResult{OK: rep.OK(), Report: rep}, nil
			})
		},
	}
	c.Flags().StringArrayVar(&links, "link", nil, "An expected link, STATION/PORT=STATION/PORT, e.g. plc/port-001=io-1/port-002 (repeatable)")
	c.Flags().StringVar(&file, "expect", "", "A JSON file with the expected links")
	return c
}

// parsePeerChecks reads peer checks given as PORT=STATION/PORT, the port
// as its number (1 for port-001).
func parsePeerChecks(specs []string) ([]controller.PeerCheck, error) {
	var out []controller.PeerCheck
	for _, s := range specs {
		n, peer, ok := strings.Cut(s, "=")
		st, p, ok2 := strings.Cut(peer, "/")
		num, err := strconv.ParseUint(strings.TrimSpace(n), 10, 8)
		if !ok || !ok2 || err != nil || num == 0 || st == "" || p == "" {
			return nil, fmt.Errorf("%q: want PORT=STATION/PORT with a port number from 1", s)
		}
		out = append(out, controller.PeerCheck{Subslot: model.SubslotPort1 + uint16(num-1), Station: st, Port: p})
	}
	return out, nil
}
