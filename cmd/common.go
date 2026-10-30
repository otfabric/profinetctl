// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/otfabric/go-profinet"
	"github.com/otfabric/go-profinet/controller"
	"github.com/otfabric/go-profinet/dcp"
	"github.com/otfabric/go-profinet/ethernet"
	"github.com/otfabric/go-profinet/pnio"
	"github.com/otfabric/go-profinet/rpc"

	"github.com/otfabric/profinetctl/internal/errs"
	"github.com/otfabric/profinetctl/internal/format"
	"github.com/otfabric/profinetctl/internal/runner"
)

// classify turns an error of go-profinet into a typed error of profinetctl, prefixed
// with what was being done.
func classify(what string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errs.As(err); ok {
		return err
	}
	msg := err.Error()
	if what != "" {
		msg = what + ": " + msg
	}
	var se *pnio.StatusError
	var de *dcp.SetError
	switch {
	case errors.As(err, &se):
		return errs.New(errs.KindRefused, errs.CodePNIOError, msg, err)
	case errors.As(err, &de):
		return errs.New(errs.KindRefused, errs.CodeDCPBlockError, msg, err)
	case errors.Is(err, context.Canceled):
		return errs.New(errs.KindInterrupted, errs.CodeContextCanceled, msg, err)
	case errors.Is(err, profinet.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return errs.New(errs.KindTimeout, errs.CodeRequestTimeout, msg, err)
	case errors.Is(err, profinet.ErrNotFound):
		return errs.New(errs.KindNotFound, errs.CodeDeviceNotFound, msg, err)
	case errors.Is(err, profinet.ErrMalformed):
		return errs.New(errs.KindProtocol, errs.CodeProtocolViolation, msg, err)
	case errors.Is(err, profinet.ErrUnsupported):
		return errs.New(errs.KindUnsupported, errs.CodeUnsupported, msg, err)
	case errors.Is(err, profinet.ErrRejected), errors.Is(err, profinet.ErrAborted):
		return errs.New(errs.KindRefused, errs.CodeConnectFailed, msg, err)
	case errors.Is(err, profinet.ErrInvalidConfig):
		return errs.InvalidInput(errs.CodeInvalidInput, msg, err)
	default:
		var ne net.Error
		if errors.As(err, &ne) || errors.Is(err, profinet.ErrLinkDown) || errors.Is(err, profinet.ErrClosed) {
			return errs.New(errs.KindTransport, errs.CodeSocketFailed, msg, err)
		}
		return errs.New(errs.KindInternal, errs.CodeInternalUnexpected, msg, err)
	}
}

// logger returns the logger of go-profinet: stderr with --debug, nothing otherwise.
func (a *app) logger() profinet.Logger {
	if !a.debug {
		return profinet.NopLogger()
	}
	return profinet.NewSlogLogger(slog.NewTextHandler(a.stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// result runs collect and writes its result in the format of the invocation.
func (a *app) result(c *cobra.Command, collect func(ctx context.Context) (any, error)) error {
	f, err := runner.Output(c.Context(), a.format)
	if err != nil {
		return err
	}
	v, err := collect(c.Context())
	if err != nil {
		return err
	}
	return runner.Write(c.Context(), c.OutOrStdout(), f, v, partialExit)
}

// partialer is a result that can be incomplete: some of its parts failed.
type partialer interface{ Partial() bool }

func partialExit(v any) int {
	if p, ok := v.(partialer); ok && p.Partial() {
		return errs.ExitPartial
	}
	return errs.ExitOK
}

// stream runs run with a stream writer on stdout.
func (a *app) stream(c *cobra.Command, run func(ctx context.Context, out *format.StreamWriter) error) error {
	f, err := runner.Output(c.Context(), a.format)
	if err != nil {
		return err
	}
	return run(c.Context(), format.NewStreamWriter(c.OutOrStdout(), f))
}

// session is an open link with a DCP requester.
type session struct {
	a    *app
	link *Link
	dcp  *dcp.Client
}

func (a *app) open() (*session, error) {
	l, err := a.openLink()
	if err != nil {
		return nil, err
	}
	c, err := dcp.NewClient(l.Mux, dcp.ClientOptions{Timeout: a.timeout / 4, Retries: 3, Logger: a.logger()})
	if err != nil {
		l.Close()
		return nil, classify("dcp", err)
	}
	return &session{a: a, link: l, dcp: c}, nil
}

func (s *session) Close() {
	_ = s.dcp.Close()
	s.link.Close()
}

// target selects one device: by station name (resolved with DCP Identify) or MAC
// address.
type target struct {
	station string
	mac     string
	ip      string
	wait    time.Duration
}

func addTargetFlags(c *cobra.Command, t *target) {
	c.Flags().StringVarP(&t.station, "station", "s", "", "Station name (NameOfStation) of the device")
	c.Flags().StringVar(&t.mac, "mac", "", "MAC address of the device")
	c.Flags().DurationVar(&t.wait, "wait", time.Second, "How long DCP Identify waits for the device to answer")
}

func addRPCTargetFlags(c *cobra.Command, t *target) {
	addTargetFlags(c, t)
	c.Flags().StringVar(&t.ip, "ip", "", "IP address of the device's RPC endpoint (default: the address it reports in DCP)")
}

func (t *target) validate() error {
	switch {
	case t.station == "" && t.mac == "":
		return errs.New(errs.KindUsage, errs.CodeInvalidInput, "no device: give --station or --mac", nil)
	case t.station != "" && t.mac != "":
		return errs.New(errs.KindUsage, errs.CodeInvalidFlagCombination, "--station and --mac select the device both: give one", nil)
	case t.mac != "":
		if _, err := ethernet.ParseMAC(t.mac); err != nil {
			return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--mac: %v", err), err)
		}
	default:
		if err := dcp.ValidateStationName(t.station); err != nil {
			return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--station %q: %v", t.station, err), err)
		}
	}
	if t.ip != "" {
		if _, err := netip.ParseAddr(t.ip); err != nil {
			return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--ip: %v", err), err)
		}
	}
	return nil
}

// describe names the target for messages.
func (t *target) describe() string {
	if t.station != "" {
		return fmt.Sprintf("station %q", t.station)
	}
	return "MAC " + t.mac
}

// matches reports whether s names the target: its station name or its MAC address.
func (t *target) matches(s string, id dcp.Identity) bool {
	if s == "" {
		return false
	}
	if m, err := ethernet.ParseMAC(s); err == nil {
		return m == id.MAC
	}
	return s == id.NameOfStation
}

// resolve finds the target and reads its identity.
func (s *session) resolve(ctx context.Context, t *target) (dcp.Identity, error) {
	if t.mac != "" {
		mac, _ := ethernet.ParseMAC(t.mac)
		cctx, cancel := context.WithTimeout(ctx, s.a.timeout)
		defer cancel()
		id, err := s.dcp.GetIdentity(cctx, mac)
		if err != nil {
			return id, classify("DCP Get of "+t.describe(), err)
		}
		return id, nil
	}
	cctx, cancel := context.WithTimeout(ctx, t.wait+s.a.timeout)
	defer cancel()
	ids, err := s.dcp.Identify(cctx, dcp.FilterName(t.station), dcp.IdentifyOptions{Wait: t.wait})
	if err != nil {
		return dcp.Identity{}, classify("DCP Identify", err)
	}
	switch len(ids) {
	case 0:
		return dcp.Identity{}, errs.New(errs.KindNotFound, errs.CodeDeviceNotFound, fmt.Sprintf("no device answers to %s within %s", t.describe(), t.wait), nil)
	case 1:
		return ids[0], nil
	default:
		var macs []string
		for _, id := range ids {
			macs = append(macs, id.MAC.String())
		}
		return dcp.Identity{}, errs.New(errs.KindRefused, errs.CodeDuplicateName,
			fmt.Sprintf("%d devices answer to %s (%s): select one with --mac", len(ids), t.describe(), strings.Join(macs, ", ")), nil)
	}
}

// rpcSession is a resolved device with an RPC client for its records.
type rpcSession struct {
	*session
	id  dcp.Identity
	cli *rpc.Client
	rc  *controller.RecordClient
}

// openRPC opens a session, resolves the target and prepares its record client.
func (a *app) openRPC(ctx context.Context, t *target) (*rpcSession, error) {
	s, err := a.open()
	if err != nil {
		return nil, err
	}
	id, err := s.resolve(ctx, t)
	if err != nil {
		s.Close()
		return nil, err
	}
	ip := id.IP.Addr
	if t.ip != "" {
		ip, _ = netip.ParseAddr(t.ip)
	}
	if !ip.IsValid() || ip.IsUnspecified() {
		s.Close()
		return nil, errs.New(errs.KindRefused, errs.CodeInvalidInput,
			fmt.Sprintf("%s has no IP address: assign one with \"profinetctl ip set\" or give --ip", t.describe()), nil)
	}
	conn, err := s.link.Listen(ctx, ":0")
	if err != nil {
		s.Close()
		return nil, errs.New(errs.KindTransport, errs.CodeSocketFailed, fmt.Sprintf("RPC socket: %v", err), err)
	}
	cli := rpc.NewClient(conn, true, rpc.ClientOptions{Timeout: a.timeout, Logger: a.logger()})
	addr := &net.UDPAddr{IP: ip.AsSlice(), Port: rpc.Port}
	rc := controller.NewRecordClient(cli, addr, pnio.ObjectUUID(max(id.Instance, 1), id.DeviceID, id.VendorID))
	return &rpcSession{session: s, id: id, cli: cli, rc: rc}, nil
}

func (s *rpcSession) Close() {
	_ = s.cli.Close()
	s.session.Close()
}

// ctx bounds one request.
func (s *rpcSession) ctx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.a.timeout)
}

// supervisor opens a device access AR for writes.
func (s *rpcSession) supervisor(ctx context.Context) (*controller.SupervisorAR, error) {
	cctx, cancel := s.ctx(ctx)
	defer cancel()
	ar, err := s.rc.OpenSupervisor(cctx, controller.SupervisorConfig{
		InitiatorMAC: s.link.MAC(), InitiatorObject: toolObject, StationName: "profinetctl",
		ActivityTimeout: max(3*s.a.timeout, 3*time.Second),
	})
	if err != nil {
		return nil, classify("device access AR (Connect)", err)
	}
	return ar, nil
}

// The identity of profinetctl as an initiator of ARs: its PNIO object UUID is
// formed from them. 0xF000 is outside PROFIBUS & PROFINET International's
// assigned vendor IDs.
const (
	toolVendorID = 0xF000
	toolDeviceID = 0x0C71
)

var toolObject = pnio.ObjectUUID(1, toolDeviceID, toolVendorID)

// address flags of one record.
type recordAddr struct {
	api     uint32
	slot    uint16
	subslot uint16
	index   string
}

func addRecordFlags(c *cobra.Command, r *recordAddr, defSlot, defSubslot uint16, indexHelp string) {
	c.Flags().Uint32Var(&r.api, "api", 0, "Application process identifier")
	c.Flags().Uint16Var(&r.slot, "slot", defSlot, "Slot number")
	c.Flags().Uint16Var(&r.subslot, "subslot", defSubslot, "Subslot number (0x8001 is the first port)")
	if indexHelp != "" {
		c.Flags().StringVar(&r.index, "index", "", indexHelp)
	}
}

func (r *recordAddr) record() (controller.Record, error) {
	idx, err := parseUint16(r.index)
	if err != nil {
		return controller.Record{}, errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--index %q: %v", r.index, err), err)
	}
	return controller.Record{API: r.api, Slot: r.slot, Subslot: r.subslot, Index: pnio.Index(idx)}, nil
}

// parseUint16 parses a decimal or 0x-prefixed number.
func parseUint16(s string) (uint16, error) {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 0, 16)
	return uint16(v), err
}

// parseHex parses octets in hex, with optional separators (spaces, ':', '-').
func parseHex(s string) ([]byte, error) {
	s = strings.NewReplacer(" ", "", ":", "", "-", "", "0x", "").Replace(strings.TrimSpace(s))
	return hex.DecodeString(s)
}

// hexString formats octets as hex for text output.
func hexString(b []byte) string {
	if len(b) == 0 {
		return "-"
	}
	return hex.EncodeToString(b)
}

// suiteOf parses an address with prefix length (192.168.0.10/24) and a gateway.
func suiteOf(addr, gateway string) (dcp.IPSuite, error) {
	pfx, err := netip.ParsePrefix(addr)
	if err != nil {
		a, aerr := netip.ParseAddr(addr)
		if aerr != nil {
			return dcp.IPSuite{}, fmt.Errorf("%q is not an address with a prefix length (192.168.0.10/24)", addr)
		}
		pfx = netip.PrefixFrom(a, 24)
	}
	if !pfx.Addr().Is4() {
		return dcp.IPSuite{}, fmt.Errorf("%q is not an IPv4 address", addr)
	}
	mask, _ := netip.AddrFromSlice(net.CIDRMask(pfx.Bits(), 32))
	gw := netip.IPv4Unspecified()
	if gateway != "" {
		if gw, err = netip.ParseAddr(gateway); err != nil || !gw.Is4() {
			return dcp.IPSuite{}, fmt.Errorf("gateway %q is not an IPv4 address", gateway)
		}
	}
	return dcp.IPSuite{Addr: pfx.Addr(), Mask: mask, Gateway: gw}, nil
}

// prefixLen returns the prefix length of a mask, -1 when it is not contiguous.
func prefixLen(mask netip.Addr) int {
	if !mask.Is4() {
		return -1
	}
	b := mask.As4()
	ones, size := net.IPMask(b[:]).Size()
	if size == 0 {
		return -1
	}
	return ones
}

// formatSuite formats an IP suite as address/prefix and gateway.
func formatSuite(s dcp.IPSuite) string {
	if !s.Addr.IsValid() || s.Addr.IsUnspecified() {
		return "-"
	}
	out := s.Addr.String()
	if n := prefixLen(s.Mask); n >= 0 {
		out += "/" + strconv.Itoa(n)
	}
	if s.Gateway.IsValid() && !s.Gateway.IsUnspecified() {
		out += " via " + s.Gateway.String()
	}
	return out
}

// orDash returns s, or "-" when it is empty.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
