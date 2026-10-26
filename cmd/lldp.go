// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/otfabric/go-profinet/controller"
	"github.com/otfabric/go-profinet/dcp"
	"github.com/otfabric/go-profinet/lldp"
	"github.com/otfabric/go-profinet/pnio"
	"github.com/otfabric/go-profinet/rpc"

	"github.com/otfabric/profinetctl/internal/errs"
)

// neighborView is an LLDP neighbor of the host.
type neighborView struct {
	MAC               string `json:"mac"`
	ChassisID         string `json:"chassis_id"`
	PortID            string `json:"port_id"`
	TTL               uint16 `json:"ttl"`
	SystemName        string `json:"system_name,omitempty"`
	SystemDescription string `json:"system_description,omitempty"`
	PortDescription   string `json:"port_description,omitempty"`
	ManagementAddress string `json:"management_address,omitempty"`
	ChassisMAC        string `json:"chassis_mac,omitempty"`
	// Alias is the PROFINET AliasName of the device connected to this
	// neighbor's port: the name a device without station name answers to.
	Alias string `json:"alias"`
	// LineDelay is the cable delay the neighbor measured, in ns.
	LineDelay *uint32 `json:"line_delay,omitempty"`
}

type neighborsResult struct {
	Interface string         `json:"interface"`
	Neighbors []neighborView `json:"neighbors"`
}

func (r neighborsResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, n := range r.Neighbors {
		fmt.Fprintf(&b, "%s  chassis %q port %q ttl %ds", n.MAC, n.ChassisID, n.PortID, n.TTL)
		if n.SystemDescription != "" {
			fmt.Fprintf(&b, "  %s", n.SystemDescription)
		}
		if n.ManagementAddress != "" {
			fmt.Fprintf(&b, "  %s", n.ManagementAddress)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "%d neighbor(s) on %s\n", len(r.Neighbors), r.Interface)
	return b.String(), nil
}

func (r neighborsResult) TableHeaders() []string {
	return []string{"MAC", "CHASSIS", "PORT", "TTL", "ADDRESS", "DESCRIPTION"}
}

func (r neighborsResult) TableRows() [][]string {
	var rows [][]string
	for _, n := range r.Neighbors {
		rows = append(rows, []string{n.MAC, n.ChassisID, n.PortID, fmt.Sprint(n.TTL), orDash(n.ManagementAddress), orDash(n.SystemDescription)})
	}
	return rows
}

func neighborViews(ns []lldp.Neighbor) []neighborView {
	out := []neighborView{}
	for _, n := range ns {
		v := neighborView{MAC: n.MAC.String(), ChassisID: n.Frame.ChassisString(), PortID: n.Frame.PortString(), TTL: n.Frame.TTL,
			SystemName: n.Frame.SystemName, SystemDescription: n.Frame.SystemDescription, PortDescription: n.Frame.PortDescription, Alias: n.Alias()}
		if n.Frame.ManagementAddr.IsValid() {
			v.ManagementAddress = n.Frame.ManagementAddr.String()
		}
		if n.Frame.ChassisMAC != nil {
			v.ChassisMAC = n.Frame.ChassisMAC.String()
		}
		if d := n.Frame.Delay; d != nil {
			c := d.Cable
			v.LineDelay = &c
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MAC < out[j].MAC })
	return out
}

// listenLLDP collects the LLDP neighbors of the link for wait.
func listenLLDP(ctx context.Context, s *session, wait time.Duration) ([]lldp.Neighbor, error) {
	ag, err := lldp.NewAgent(s.link.Mux, lldp.AgentOptions{Interval: -1, Logger: s.a.logger()})
	if err != nil {
		return nil, classify("LLDP", err)
	}
	defer ag.Close()
	select {
	case <-time.After(wait):
	case <-ctx.Done():
		if ctx.Err() == context.Canceled {
			return nil, classify("LLDP", ctx.Err())
		}
	}
	return ag.Neighbors(), nil
}

func newLLDPCmd(a *app) *cobra.Command {
	g := groupCmd("lldp", "Link Layer Discovery Protocol")
	var wait time.Duration
	neighbors := &cobra.Command{
		Use:   "neighbors",
		Short: "Listen for LLDP and list the neighbors of the interface",
		Long: `Listen for LLDP frames on the interface for --wait and list the neighbors that
sent one: the switch port or device the host is connected to. PROFINET devices
send LLDP every 5 seconds. profinetctl only listens; it sends no LLDP.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.open()
				if err != nil {
					return nil, err
				}
				defer s.Close()
				ns, err := listenLLDP(ctx, s, wait)
				if err != nil {
					return nil, err
				}
				return neighborsResult{Interface: a.iface, Neighbors: neighborViews(ns)}, nil
			})
		},
	}
	neighbors.Flags().DurationVar(&wait, "wait", 6*time.Second, "How long to listen")
	g.AddCommand(neighbors)
	return g
}

// topoNode is a device of the topology.
type topoNode struct {
	Station string     `json:"station"`
	MAC     string     `json:"mac"`
	IP      string     `json:"ip"`
	Ports   []topoPort `json:"ports"`
	Error   string     `json:"error,omitempty"`
	rc      *controller.RecordClient
}

type topoPort struct {
	Subslot uint16 `json:"subslot"`
	PortID  string `json:"port_id"`
	Link    string `json:"link"`
	// Peers are the neighbors the port reports: one behind a switch, more
	// on a shared segment.
	Peers []topoPeer `json:"peers"`
}

// topoPeer is a neighbor of a port, as its LLDP reports it.
type topoPeer struct {
	Station   string `json:"station"`
	Port      string `json:"port"`
	MAC       string `json:"mac"`
	LineDelay uint32 `json:"line_delay,omitempty"`
}

func (p topoPeer) String() string { return strings.TrimSpace(p.Station + " " + p.Port) }

func peersString(ps []topoPeer) string {
	var s []string
	for _, p := range ps {
		s = append(s, p.String())
	}
	return orDash(strings.Join(s, ", "))
}

// topoLink is a cable between two ports.
type topoLink struct {
	A     string `json:"a"`
	APort string `json:"a_port"`
	B     string `json:"b"`
	BPort string `json:"b_port"`
}

type topologyResult struct {
	// Host is what the host's own interface sees with LLDP.
	Host  []neighborView `json:"host_neighbors"`
	Nodes []topoNode     `json:"devices"`
	Links []topoLink     `json:"links"`
}

func (r topologyResult) Partial() bool {
	for _, n := range r.Nodes {
		if n.Error != "" {
			return true
		}
	}
	return false
}

func (r topologyResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, n := range r.Host {
		fmt.Fprintf(&b, "this host ── %s %s\n", n.ChassisID, n.PortID)
	}
	for _, n := range r.Nodes {
		fmt.Fprintf(&b, "%s (%s, %s)\n", orDash(n.Station), n.MAC, n.IP)
		if n.Error != "" {
			fmt.Fprintf(&b, "  error: %s\n", n.Error)
		}
		for _, p := range n.Ports {
			fmt.Fprintf(&b, "  %-10s link %-5s ── %s\n", p.PortID, p.Link, peersString(p.Peers))
		}
	}
	if len(r.Links) > 0 {
		b.WriteString("Links:\n")
		for _, l := range r.Links {
			fmt.Fprintf(&b, "  %s %s ── %s %s\n", l.A, l.APort, l.B, l.BPort)
		}
	}
	return b.String(), nil
}

func (r topologyResult) TableHeaders() []string {
	return []string{"STATION", "PORT", "LINK", "PEERS"}
}

func (r topologyResult) TableRows() [][]string {
	var rows [][]string
	for _, n := range r.Nodes {
		for _, p := range n.Ports {
			rows = append(rows, []string{orDash(n.Station), p.PortID, p.Link, peersString(p.Peers)})
		}
		if len(n.Ports) == 0 {
			rows = append(rows, []string{orDash(n.Station), "-", "-", orDash(n.Error)})
		}
	}
	return rows
}

// peerStation returns the station name of a peer. PROFINET devices put it in
// their port ID after the port name ("port-001.io-1"); the chassis ID is the
// station name for some and the system description for others (p-net).
func peerStation(p pnio.PDPortPeer) string {
	if i := strings.IndexByte(p.PortID, '.'); i >= 0 {
		return p.PortID[i+1:]
	}
	return p.ChassisID
}

func peerPort(p pnio.PDPortPeer) string {
	if i := strings.IndexByte(p.PortID, '.'); i >= 0 {
		return p.PortID[:i]
	}
	return p.PortID
}

// links derives the cables from the port data: every peer relation, once.
func links(nodes []topoNode) []topoLink {
	seen := map[string]bool{}
	out := []topoLink{}
	for _, n := range nodes {
		for _, p := range n.Ports {
			for _, peer := range p.Peers {
				// A device that lists itself (its own LLDP reflected back to
				// it) is not a cable.
				if peer.Station == "" || peer.Station == n.Station {
					continue
				}
				a, b := n.Station+" "+p.PortID, peer.Station+" "+peer.Port
				key := a + "|" + b
				if a > b {
					key = b + "|" + a
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, topoLink{A: n.Station, APort: p.PortID, B: peer.Station, BPort: peer.Port})
			}
		}
	}
	return out
}

func newTopologyCmd(a *app) *cobra.Command {
	var wait, lldpWait time.Duration
	c := &cobra.Command{
		Use:   "topology",
		Short: "Discover the devices and read their port neighbors (PDRealData) to map the cabling",
		Long: `Find every device with DCP Identify, read the port data of each (PDRealData,
index 0xF841: the LLDP neighbor of every port) and derive the links between
them. With --lldp-wait the host also listens for LLDP to place itself. Devices
without an IP address cannot be read. Exit code 7 when some could not be read.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.open()
				if err != nil {
					return nil, err
				}
				defer s.Close()
				r := topologyResult{Host: []neighborView{}, Nodes: []topoNode{}, Links: []topoLink{}}
				var hostNs []lldp.Neighbor
				var wg sync.WaitGroup
				if lldpWait > 0 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						hostNs, _ = listenLLDP(ctx, s, lldpWait)
					}()
				}
				ids, err := s.dcp.Identify(ctx, dcp.FilterAll(), dcp.IdentifyOptions{Wait: wait, ResponseDelay: 10})
				if err != nil {
					wg.Wait()
					return nil, classify("DCP Identify", err)
				}
				sort.Slice(ids, func(i, j int) bool { return ids[i].NameOfStation < ids[j].NameOfStation })
				conn, err := s.link.Listen(ctx, ":0")
				if err != nil {
					wg.Wait()
					return nil, errs.New(errs.KindTransport, errs.CodeSocketFailed, fmt.Sprintf("RPC socket: %v", err), err)
				}
				cli := rpc.NewClient(conn, true, rpc.ClientOptions{Timeout: a.timeout, Activities: 8, Logger: a.logger()})
				defer func() { _ = cli.Close() }()
				nodes := make([]topoNode, len(ids))
				var nwg sync.WaitGroup
				for i, id := range ids {
					nodes[i] = topoNode{Station: id.NameOfStation, MAC: id.MAC.String(), IP: addrOr(id.IP.Addr), Ports: []topoPort{}}
					if !id.IP.Addr.IsValid() || id.IP.Addr.IsUnspecified() {
						nodes[i].Error = "no IP address: the port data cannot be read"
						continue
					}
					addr := &net.UDPAddr{IP: id.IP.Addr.AsSlice(), Port: rpc.Port}
					nodes[i].rc = controller.NewRecordClient(cli, addr, pnio.ObjectUUID(max(id.Instance, 1), id.DeviceID, id.VendorID))
					nwg.Add(1)
					go func(n *topoNode) {
						defer nwg.Done()
						cctx, cancel := context.WithTimeout(ctx, a.timeout)
						defer cancel()
						b, err := n.rc.Read(cctx, rpc.NilUUID, controller.Record{Slot: 0, Subslot: 1, Index: pnio.IndexPDRealData}, 0)
						if err != nil {
							n.Error = "PDRealData: " + errorOf(err)
							return
						}
						ports, err := pnio.DecodePDRealData(b)
						if err != nil {
							n.Error = "PDRealData: " + errorOf(err)
						}
						for _, p := range ports {
							tp := topoPort{Subslot: p.Subslot, PortID: p.OwnPortID, Link: linkName(p.LinkState), Peers: []topoPeer{}}
							for _, peer := range p.Peers {
								tp.Peers = append(tp.Peers, topoPeer{Station: peerStation(peer), Port: peerPort(peer), MAC: peer.MAC.String(), LineDelay: peer.LineDelay})
							}
							n.Ports = append(n.Ports, tp)
						}
					}(&nodes[i])
				}
				nwg.Wait()
				wg.Wait()
				r.Nodes = nodes
				r.Links = links(nodes)
				r.Host = neighborViews(hostNs)
				return r, nil
			})
		},
	}
	c.Flags().DurationVar(&wait, "wait", 2*time.Second, "How long DCP Identify collects responses")
	c.Flags().DurationVar(&lldpWait, "lldp-wait", 0, "Also listen this long for the host's own LLDP neighbor")
	return c
}
