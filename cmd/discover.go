// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/otfabric/go-profinet/dcp"
	"github.com/otfabric/go-profinet/l2/osport"

	"github.com/otfabric/profinetctl/internal/errs"
)

// interfacesResult lists the host's interfaces.
type interfacesResult struct {
	Backend    string             `json:"backend"`
	Interfaces []osport.Interface `json:"interfaces"`
}

func (r interfacesResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, i := range r.Interfaces {
		state := "down"
		if i.Up {
			state = "up"
		}
		fmt.Fprintf(&b, "%s  %s  mtu %d  %s  %s\n", i.Name, i.MAC, i.MTU, state, orDash(strings.Join(i.Addrs, ", ")))
	}
	if len(r.Interfaces) == 0 {
		b.WriteString("no Ethernet interfaces\n")
	}
	return b.String(), nil
}

func (r interfacesResult) TableHeaders() []string {
	return []string{"NAME", "MAC", "MTU", "STATE", "ADDRESSES"}
}

func (r interfacesResult) TableRows() [][]string {
	var rows [][]string
	for _, i := range r.Interfaces {
		state := "down"
		if i.Up {
			state = "up"
		}
		rows = append(rows, []string{i.Name, i.MAC, fmt.Sprint(i.MTU), state, orDash(strings.Join(i.Addrs, ", "))})
	}
	return rows
}

func newInterfacesCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "interfaces",
		Short: "List the Ethernet interfaces profinetctl can use",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return a.result(c, func(context.Context) (any, error) {
				ifs, err := a.backend.Interfaces()
				if err != nil {
					return nil, errs.New(errs.KindTransport, errs.CodeInterfaceOpenFailed, fmt.Sprintf("list interfaces: %v", err), err)
				}
				return interfacesResult{Backend: a.backend.Name(), Interfaces: ifs}, nil
			})
		},
	}
}

// deviceView is a device as DCP reports it.
type deviceView struct {
	MAC           string   `json:"mac"`
	NameOfStation string   `json:"name_of_station"`
	TypeOfStation string   `json:"type_of_station,omitempty"`
	VendorID      uint16   `json:"vendor_id"`
	DeviceID      uint16   `json:"device_id"`
	Instance      *uint16  `json:"instance,omitempty"`
	Roles         []string `json:"roles"`
	IP            string   `json:"ip"`
	Mask          string   `json:"mask"`
	Gateway       string   `json:"gateway"`
	IPSetBy       string   `json:"ip_set_by"`
	IPConflict    bool     `json:"ip_conflict,omitempty"`
	OEMVendorID   *uint16  `json:"oem_vendor_id,omitempty"`
	OEMDeviceID   *uint16  `json:"oem_device_id,omitempty"`
	Options       []string `json:"options,omitempty"`
}

func roles(r uint8) []string {
	var out []string
	for _, x := range []struct {
		bit  uint8
		name string
	}{{dcp.RoleIODevice, "IO-Device"}, {dcp.RoleIOController, "IO-Controller"}, {dcp.RoleIOMultidev, "IO-Multidevice"}, {dcp.RolePNSupervisor, "PN-Supervisor"}} {
		if r&x.bit != 0 {
			out = append(out, x.name)
		}
	}
	return out
}

func ipSetBy(info uint16) string {
	switch info & 0x0003 {
	case 0:
		return "none"
	case 1:
		return "static"
	default:
		return "dhcp"
	}
}

func viewOf(id dcp.Identity) deviceView {
	v := deviceView{
		MAC: id.MAC.String(), NameOfStation: id.NameOfStation, TypeOfStation: id.TypeOfStation,
		VendorID: id.VendorID, DeviceID: id.DeviceID, Roles: roles(id.Role),
		IP: addrOr(id.IP.Addr), Mask: addrOr(id.IP.Mask), Gateway: addrOr(id.IP.Gateway),
		IPSetBy: ipSetBy(id.IPInfo), IPConflict: id.IPInfo&dcp.IPInfoConflict != 0,
	}
	if id.HasInstance {
		inst := id.Instance
		v.Instance = &inst
	}
	if id.HasOEM {
		ov, od := id.OEMVendorID, id.OEMDeviceID
		v.OEMVendorID, v.OEMDeviceID = &ov, &od
	}
	for _, o := range id.Options {
		v.Options = append(v.Options, o.String())
	}
	if v.Roles == nil {
		v.Roles = []string{}
	}
	return v
}

func addrOr(a netip.Addr) string {
	if !a.IsValid() {
		return "0.0.0.0"
	}
	return a.String()
}

func (v deviceView) row() []string {
	return []string{v.MAC, orDash(v.NameOfStation), fmt.Sprintf("0x%04X", v.VendorID), fmt.Sprintf("0x%04X", v.DeviceID),
		v.IP, orDash(v.TypeOfStation), orDash(strings.Join(v.Roles, ","))}
}

var deviceHeaders = []string{"MAC", "STATION", "VENDOR", "DEVICE", "IP", "TYPE", "ROLE"}

// discoverResult is the devices that answered an Identify.
type discoverResult struct {
	Devices []deviceView `json:"devices"`
	// Duplicates lists station names that more than one device answers to.
	Duplicates []string `json:"duplicate_names,omitempty"`
}

func (r discoverResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, d := range r.Devices {
		fmt.Fprintf(&b, "%s  %-24s vendor 0x%04X device 0x%04X  %s  %s\n", d.MAC, orDash(d.NameOfStation), d.VendorID, d.DeviceID, d.IP, orDash(d.TypeOfStation))
	}
	fmt.Fprintf(&b, "%d device(s)\n", len(r.Devices))
	for _, n := range r.Duplicates {
		fmt.Fprintf(&b, "warning: station name %q is used by more than one device\n", n)
	}
	return b.String(), nil
}

func (r discoverResult) TableHeaders() []string { return deviceHeaders }

func (r discoverResult) TableRows() [][]string {
	var rows [][]string
	for _, d := range r.Devices {
		rows = append(rows, d.row())
	}
	return rows
}

func newDiscoverCmd(a *app, name string) *cobra.Command {
	var (
		wait           time.Duration
		station, alias string
		ip             string
		vendor, device string
		delay          uint16
	)
	c := &cobra.Command{
		Use:   name,
		Short: "Find the PROFINET devices on the network (DCP Identify)",
		Long: `Multicast a DCP Identify request and list the devices that answer, sorted by MAC
address. Without a filter every device answers; --station, --alias, --ip and
--vendor/--device restrict the request to the devices that match.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			filter := dcp.FilterAll()
			var wantIP netip.Addr
			switch {
			case station != "":
				filter = dcp.FilterName(station)
			case alias != "":
				filter = dcp.FilterAlias(alias)
			case ip != "":
				// DCP's IP filter compares the mask and gateway too: the
				// address is matched here.
				a, err := netip.ParseAddr(ip)
				if err != nil {
					return errs.InvalidInput(errs.CodeInvalidInput, "--ip: "+err.Error(), err)
				}
				wantIP = a
			case vendor != "" || device != "":
				v, verr := parseUint16(vendor)
				d, derr := parseUint16(device)
				if verr != nil || derr != nil {
					return errs.InvalidInput(errs.CodeInvalidInput, "--vendor and --device take a 16-bit ID each (0x002A)", nil)
				}
				filter = dcp.FilterDeviceID(v, d)
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.open()
				if err != nil {
					return nil, err
				}
				defer s.Close()
				ids, err := s.dcp.Identify(ctx, filter, dcp.IdentifyOptions{Wait: wait, ResponseDelay: delay})
				if err != nil {
					return nil, classify("DCP Identify", err)
				}
				sort.Slice(ids, func(i, j int) bool { return ids[i].MAC.String() < ids[j].MAC.String() })
				res := discoverResult{Devices: []deviceView{}}
				names := map[string]int{}
				for _, id := range ids {
					if wantIP.IsValid() && id.IP.Addr != wantIP {
						continue
					}
					res.Devices = append(res.Devices, viewOf(id))
					if id.NameOfStation != "" {
						names[id.NameOfStation]++
					}
				}
				for n, k := range names {
					if k > 1 {
						res.Duplicates = append(res.Duplicates, n)
					}
				}
				sort.Strings(res.Duplicates)
				return res, nil
			})
		},
	}
	if name == "devices" {
		c.Short = "List the PROFINET devices on the network (same as discover)"
	}
	f := c.Flags()
	f.DurationVar(&wait, "wait", 2*time.Second, "How long to collect responses")
	f.Uint16Var(&delay, "response-delay", 10, "ResponseDelayFactor: devices spread their answers over this × 10 ms")
	f.StringVarP(&station, "station", "s", "", "Only the device with this station name")
	f.StringVar(&alias, "alias", "", "Only the device with this alias name (port-001.neighbor)")
	f.StringVar(&ip, "ip", "", "Only the device with this IP address")
	f.StringVar(&vendor, "vendor", "", "Only devices with this vendor ID (with --device)")
	f.StringVar(&device, "device", "", "Only devices with this device ID (with --vendor)")
	return c
}

// identifyResult is the identity of one device.
type identifyResult struct {
	Device deviceView `json:"device"`
}

func (r identifyResult) MarshalTextOutput() (string, error) {
	d := r.Device
	var b strings.Builder
	fmt.Fprintf(&b, "MAC:              %s\n", d.MAC)
	fmt.Fprintf(&b, "NameOfStation:    %s\n", orDash(d.NameOfStation))
	fmt.Fprintf(&b, "TypeOfStation:    %s\n", orDash(d.TypeOfStation))
	fmt.Fprintf(&b, "Vendor/Device ID: 0x%04X / 0x%04X\n", d.VendorID, d.DeviceID)
	if d.Instance != nil {
		fmt.Fprintf(&b, "Instance:         %d\n", *d.Instance)
	}
	if d.OEMVendorID != nil {
		fmt.Fprintf(&b, "OEM vendor/device: 0x%04X / 0x%04X\n", *d.OEMVendorID, *d.OEMDeviceID)
	}
	fmt.Fprintf(&b, "Role:             %s\n", orDash(strings.Join(d.Roles, ", ")))
	fmt.Fprintf(&b, "IP:               %s mask %s gateway %s (%s)\n", d.IP, d.Mask, d.Gateway, d.IPSetBy)
	if d.IPConflict {
		b.WriteString("IP conflict:      yes\n")
	}
	if len(d.Options) > 0 {
		fmt.Fprintf(&b, "Options:          %s\n", strings.Join(d.Options, ", "))
	}
	return b.String(), nil
}

func (r identifyResult) TableHeaders() []string { return deviceHeaders }
func (r identifyResult) TableRows() [][]string  { return [][]string{r.Device.row()} }

func newIdentifyCmd(a *app) *cobra.Command {
	var t target
	c := &cobra.Command{
		Use:   "identify",
		Short: "Show the DCP identity of one device",
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
				if t.station != "" {
					// The Identify response holds what the device chose to send; Get
					// reads every identity block.
					cctx, cancel := context.WithTimeout(ctx, a.timeout)
					defer cancel()
					if full, err := s.dcp.GetIdentity(cctx, id.MAC); err == nil {
						if len(full.Options) == 0 {
							full.Options = id.Options
						}
						id = full
					}
				}
				if len(id.Options) == 0 {
					// The list of supported options is a block of its own,
					// which some devices do not answer for Get.
					cctx, cancel := context.WithTimeout(ctx, a.timeout)
					defer cancel()
					if blocks, err := s.dcp.Get(cctx, id.MAC, dcp.DeviceOpts); err == nil {
						if o, err := dcp.ParseIdentity(id.MAC, blocks); err == nil {
							id.Options = o.Options
						}
					}
				}
				return identifyResult{Device: viewOf(id)}, nil
			})
		},
	}
	addTargetFlags(c, &t)
	return c
}
