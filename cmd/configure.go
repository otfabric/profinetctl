// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/otfabric/go-profinet/dcp"

	clihelp "github.com/otfabric/profinetctl/internal/cli"
	"github.com/otfabric/profinetctl/internal/errs"
)

// changeResult reports a change made to a device.
type changeResult struct {
	MAC       string `json:"mac"`
	Station   string `json:"station"`
	Operation string `json:"operation"`
	// Value is what was set: the name, the IP suite or the reset mode.
	Value     string `json:"value,omitempty"`
	Permanent *bool  `json:"permanent,omitempty"`
}

func (r changeResult) MarshalTextOutput() (string, error) {
	s := fmt.Sprintf("%s: %s %s", r.Operation, r.MAC, orDash(r.Station))
	if r.Value != "" {
		s += " → " + r.Value
	}
	if r.Permanent != nil {
		if *r.Permanent {
			s += " (permanent)"
		} else {
			s += " (temporary: lost on power cycle)"
		}
	}
	return s + "\n", nil
}

func (r changeResult) TableHeaders() []string {
	return []string{"MAC", "STATION", "OPERATION", "VALUE", "PERMANENT"}
}

func (r changeResult) TableRows() [][]string {
	perm := "-"
	if r.Permanent != nil {
		perm = fmt.Sprint(*r.Permanent)
	}
	return [][]string{{r.MAC, orDash(r.Station), r.Operation, orDash(r.Value), perm}}
}

// nameResult is the station name of a device.
type nameResult struct {
	MAC  string `json:"mac"`
	Name string `json:"name_of_station"`
}

func (r nameResult) MarshalTextOutput() (string, error) { return orDash(r.Name) + "\n", nil }
func (r nameResult) TableHeaders() []string             { return []string{"MAC", "STATION"} }
func (r nameResult) TableRows() [][]string              { return [][]string{{r.MAC, orDash(r.Name)}} }

// ipResult is the IP suite of a device.
type ipResult struct {
	MAC        string `json:"mac"`
	IP         string `json:"ip"`
	Mask       string `json:"mask"`
	Gateway    string `json:"gateway"`
	SetBy      string `json:"set_by"`
	IPConflict bool   `json:"ip_conflict,omitempty"`
}

func (r ipResult) MarshalTextOutput() (string, error) {
	s := fmt.Sprintf("%s mask %s gateway %s (%s)", r.IP, r.Mask, r.Gateway, r.SetBy)
	if r.IPConflict {
		s += " CONFLICT"
	}
	return s + "\n", nil
}
func (r ipResult) TableHeaders() []string { return []string{"MAC", "IP", "MASK", "GATEWAY", "SET BY"} }
func (r ipResult) TableRows() [][]string {
	return [][]string{{r.MAC, r.IP, r.Mask, r.Gateway, r.SetBy}}
}

func groupCmd(use, short string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Args: clihelp.GroupArgs, RunE: func(c *cobra.Command, _ []string) error { return c.Help() }}
}

func newNameCmd(a *app) *cobra.Command {
	g := groupCmd("name", "Read or assign the station name of a device (DCP)")
	var gt target
	get := &cobra.Command{
		Use:   "get",
		Short: "Read the station name of a device",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := gt.validate(); err != nil {
				return err
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.open()
				if err != nil {
					return nil, err
				}
				defer s.Close()
				id, err := s.resolve(ctx, &gt)
				if err != nil {
					return nil, err
				}
				return nameResult{MAC: id.MAC.String(), Name: id.NameOfStation}, nil
			})
		},
	}
	addTargetFlags(get, &gt)
	var st target
	var permanent, clear bool
	set := &cobra.Command{
		Use:   "set NAME",
		Short: "Assign a station name (DCP Set); --clear removes it",
		Long: `Assign the station name NAME to the device selected by --mac or by its current
--station name. The name must be a valid NameOfStation (lowercase DNS labels,
at most 240 characters). It is temporary unless --permanent is given: the
device forgets it at the next power cycle. --clear removes the name instead.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := st.validate(); err != nil {
				return err
			}
			name := ""
			switch {
			case clear && len(args) == 1:
				return errs.New(errs.KindUsage, errs.CodeInvalidFlagCombination, "give NAME or --clear, not both", nil)
			case !clear && len(args) == 0:
				return errs.New(errs.KindUsage, errs.CodeInvalidInput, "no name: give NAME (or --clear to remove the name)", nil)
			case !clear:
				name = args[0]
				if err := dcp.ValidateStationName(name); err != nil {
					return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("%q: %v", name, err), err)
				}
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.open()
				if err != nil {
					return nil, err
				}
				defer s.Close()
				id, err := s.resolve(ctx, &st)
				if err != nil {
					return nil, err
				}
				cctx, cancel := context.WithTimeout(ctx, a.timeout)
				defer cancel()
				if err := s.dcp.SetName(cctx, id.MAC, name, permanent); err != nil {
					return nil, classify("DCP Set NameOfStation", err)
				}
				op := "name set"
				if clear {
					op = "name cleared"
				}
				return changeResult{MAC: id.MAC.String(), Station: id.NameOfStation, Operation: op, Value: name, Permanent: &permanent}, nil
			})
		},
	}
	addTargetFlags(set, &st)
	set.Flags().BoolVar(&permanent, "permanent", false, "Store the name permanently")
	set.Flags().BoolVar(&clear, "clear", false, "Remove the station name")
	g.AddCommand(get, set)
	return g
}

func newIPCmd(a *app) *cobra.Command {
	g := groupCmd("ip", "Read or assign the IP address of a device (DCP)")
	var gt target
	get := &cobra.Command{
		Use:   "get",
		Short: "Read the IP suite of a device",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := gt.validate(); err != nil {
				return err
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.open()
				if err != nil {
					return nil, err
				}
				defer s.Close()
				id, err := s.resolve(ctx, &gt)
				if err != nil {
					return nil, err
				}
				v := viewOf(id)
				return ipResult{MAC: v.MAC, IP: v.IP, Mask: v.Mask, Gateway: v.Gateway, SetBy: v.IPSetBy, IPConflict: v.IPConflict}, nil
			})
		},
	}
	addTargetFlags(get, &gt)
	var st target
	var permanent, clear bool
	var gateway string
	set := &cobra.Command{
		Use:   "set ADDRESS/PREFIX",
		Short: "Assign an IP address (DCP Set); --clear removes it",
		Long: `Assign the IPv4 address ADDRESS/PREFIX (192.168.0.10/24; /24 when the prefix is
left out) and the optional --gateway to the device. It is temporary unless
--permanent is given. --clear sets 0.0.0.0, which removes the address.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := st.validate(); err != nil {
				return err
			}
			var suite dcp.IPSuite
			switch {
			case clear && len(args) == 1:
				return errs.New(errs.KindUsage, errs.CodeInvalidFlagCombination, "give ADDRESS or --clear, not both", nil)
			case !clear && len(args) == 0:
				return errs.New(errs.KindUsage, errs.CodeInvalidInput, "no address: give ADDRESS/PREFIX (or --clear to remove the address)", nil)
			case clear:
				suite, _ = suiteOf("0.0.0.0/0", "")
			default:
				var err error
				if suite, err = suiteOf(args[0], gateway); err != nil {
					return errs.InvalidInput(errs.CodeInvalidInput, err.Error(), err)
				}
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.open()
				if err != nil {
					return nil, err
				}
				defer s.Close()
				id, err := s.resolve(ctx, &st)
				if err != nil {
					return nil, err
				}
				cctx, cancel := context.WithTimeout(ctx, a.timeout)
				defer cancel()
				if err := s.dcp.SetIP(cctx, id.MAC, suite, permanent); err != nil {
					return nil, classify("DCP Set IP", err)
				}
				op, val := "ip set", formatSuite(suite)
				if clear {
					op, val = "ip cleared", ""
				}
				return changeResult{MAC: id.MAC.String(), Station: id.NameOfStation, Operation: op, Value: val, Permanent: &permanent}, nil
			})
		},
	}
	addTargetFlags(set, &st)
	set.Flags().BoolVar(&permanent, "permanent", false, "Store the address permanently")
	set.Flags().BoolVar(&clear, "clear", false, "Remove the IP address")
	set.Flags().StringVar(&gateway, "gateway", "", "Default gateway")
	g.AddCommand(get, set)
	return g
}

func newSignalCmd(a *app) *cobra.Command {
	var t target
	c := &cobra.Command{
		Use:   "signal",
		Short: "Make a device flash its signal LED (DCP Set Control/Signal)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := t.validate(); err != nil {
				return err
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.open()
				if err != nil {
					return nil, err
				}
				defer s.Close()
				id, err := s.resolve(ctx, &t)
				if err != nil {
					return nil, err
				}
				cctx, cancel := context.WithTimeout(ctx, a.timeout)
				defer cancel()
				if err := s.dcp.Signal(cctx, id.MAC); err != nil {
					return nil, classify("DCP Signal", err)
				}
				return changeResult{MAC: id.MAC.String(), Station: id.NameOfStation, Operation: "signal"}, nil
			})
		},
	}
	addTargetFlags(c, &t)
	return c
}

// resetModes are the ResetToFactory modes by name.
var resetModes = map[string]uint16{
	"application":   dcp.ResetApplicationData,
	"communication": dcp.ResetCommunication,
	"engineering":   dcp.ResetEngineering,
	"all-data":      dcp.ResetAllData,
	"device":        dcp.ResetDevice,
	"factory":       dcp.ResetAndRestoreDevice,
}

func resetModeNames() []string {
	var out []string
	for k := range resetModes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func newResetCmd(a *app) *cobra.Command {
	var t target
	var mode, confirm string
	c := &cobra.Command{
		Use:   "reset",
		Short: "Reset a device to its factory settings (DCP ResetToFactory); needs --confirm",
		Long: `Reset the device with DCP Control/ResetToFactory. This erases configuration of the
device and interrupts its communication: an application relationship with it is
aborted. The mode selects what is reset:

  application    the parameters of the application
  communication  the communication parameters: station name, IP address (default)
  engineering    the engineering data
  all-data       all stored data
  device         the device, keeping its firmware
  factory        the device to its factory state

--confirm must repeat the target's station name or MAC address, so that a
reset never hits a device by accident.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := t.validate(); err != nil {
				return err
			}
			m, ok := resetModes[mode]
			if !ok {
				return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--mode %q: one of %s", mode, strings.Join(resetModeNames(), ", ")), nil)
			}
			if confirm == "" {
				return errs.New(errs.KindUsage, errs.CodeConfirmationRequired,
					"reset erases the device's configuration: repeat its station name or MAC address with --confirm", nil)
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.open()
				if err != nil {
					return nil, err
				}
				defer s.Close()
				id, err := s.resolve(ctx, &t)
				if err != nil {
					return nil, err
				}
				if !t.matches(confirm, id) {
					return nil, errs.New(errs.KindUsage, errs.CodeConfirmationRequired,
						fmt.Sprintf("--confirm %q names neither the station name %q nor the MAC address %s of the target", confirm, id.NameOfStation, id.MAC), nil)
				}
				cctx, cancel := context.WithTimeout(ctx, a.timeout)
				defer cancel()
				if err := s.dcp.ResetToFactory(cctx, id.MAC, m); err != nil {
					return nil, classify("DCP ResetToFactory", err)
				}
				return changeResult{MAC: id.MAC.String(), Station: id.NameOfStation, Operation: "reset", Value: mode}, nil
			})
		},
	}
	addTargetFlags(c, &t)
	c.Flags().StringVar(&mode, "mode", "communication", "What to reset: "+strings.Join(resetModeNames(), ", "))
	c.Flags().StringVar(&confirm, "confirm", "", "The target's station name or MAC address, repeated to confirm the reset")
	_ = clihelp.RegisterEnumFlagCompletion(c, "mode", resetModeNames())
	return c
}
