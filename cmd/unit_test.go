// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"runtime/debug"
	"strings"
	"syscall"
	"testing"

	"github.com/otfabric/go-profinet"
	"github.com/otfabric/go-profinet/dcp"
	"github.com/otfabric/go-profinet/ethernet"
	"github.com/otfabric/go-profinet/l2"
	"github.com/otfabric/go-profinet/pcap"
	"github.com/otfabric/go-profinet/pnio"
	"github.com/otfabric/go-profinet/vnet"

	"github.com/otfabric/profinetctl/internal/errs"
)

type netErr struct{}

func (netErr) Error() string   { return "connection refused" }
func (netErr) Timeout() bool   { return false }
func (netErr) Temporary() bool { return false }

var _ net.Error = netErr{}

func TestClassify(t *testing.T) {
	t.Parallel()
	if classify("x", nil) != nil {
		t.Fatal("nil")
	}
	typed := errs.New(errs.KindUsage, "c", "m", nil)
	if classify("x", typed) != typed {
		t.Fatal("typed error changed")
	}
	for err, want := range map[error]errs.Kind{
		&pnio.StatusError{Op: "read", Status: pnio.RWStatus(false, pnio.RWInvalidIndex, 0)}: errs.KindRefused,
		&dcp.SetError{Block: dcp.DeviceName, Code: dcp.BlockErrorSetNotPossible}:            errs.KindRefused,
		context.Canceled:                         errs.KindInterrupted,
		context.DeadlineExceeded:                 errs.KindTimeout,
		fmt.Errorf("a: %w", profinet.ErrTimeout): errs.KindTimeout,
		profinet.ErrNotFound:                     errs.KindNotFound,
		profinet.ErrMalformed:                    errs.KindProtocol,
		profinet.ErrUnsupported:                  errs.KindUnsupported,
		profinet.ErrRejected:                     errs.KindRefused,
		profinet.ErrAborted:                      errs.KindRefused,
		profinet.ErrInvalidConfig:                errs.KindInvalidInput,
		profinet.ErrLinkDown:                     errs.KindTransport,
		profinet.ErrClosed:                       errs.KindTransport,
		netErr{}:                                 errs.KindTransport,
		errors.New("odd"):                        errs.KindInternal,
	} {
		e, ok := errs.As(classify("op", err))
		if !ok || e.Kind != want || !strings.HasPrefix(e.Message, "op: ") || !errors.Is(e, err) {
			t.Errorf("%v: %+v", err, e)
		}
	}
	if e, _ := errs.As(classify("", errors.New("bare"))); e.Message != "bare" {
		t.Errorf("message %q", e.Message)
	}
	if errorOf(nil) != "" || errorOf(&pnio.StatusError{Op: "read"}) == "" || errorOf(profinet.ErrTimeout) == "" {
		t.Error("errorOf")
	}
}

func TestVersionOf(t *testing.T) {
	t.Parallel()
	info := &debug.BuildInfo{Main: debug.Module{Version: "v0.3.1"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "0123456789abcdef"}}}
	if v, c := versionOf("dev", "", info); v != "v0.3.1" || c != "0123456" {
		t.Fatalf("from build info: %s %s", v, c)
	}
	if v, c := versionOf("v1.0.0", "abc", info); v != "v1.0.0" || c != "abc" {
		t.Fatalf("from ldflags: %s %s", v, c)
	}
	if v, c := versionOf(" dev ", "", nil); v != "dev" || c != "" {
		t.Fatalf("without build info: %s %s", v, c)
	}
	if v, _ := versionOf("", "", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}); v != "" {
		t.Fatalf("devel: %q", v)
	}
}

func TestDifferences(t *testing.T) {
	t.Parallel()
	if differences(nil) != nil {
		t.Fatal("nil diff")
	}
	diff := &pnio.ModuleDiffBlock{APIs: []pnio.DiffAPI{{Modules: []pnio.DiffModule{
		{Slot: 0, State: pnio.ModuleStateProper, Submodules: []pnio.DiffSubmodule{{Subslot: 1, State: pnio.SubmStateARInfoAppReady}}},
		{Slot: 1, State: pnio.ModuleStateNoModule},
		{Slot: 2, State: pnio.ModuleStateWrong, IdentNumber: 7},
		{Slot: 3, State: pnio.ModuleStateSubstitute, IdentNumber: 8, Submodules: []pnio.DiffSubmodule{
			{Subslot: 1, State: pnio.SubmStateFormat | pnio.SubmStateIdentWrong},
			{Subslot: 2, State: pnio.SubmStateFormat | pnio.SubmStateIdentNoSubmodule},
			{Subslot: 3, State: pnio.SubmStateFormat | pnio.SubmStateIdentSubstitute},
			{Subslot: 4, State: pnio.SubmStateFormat | pnio.SubmStateIdentOK},
		}},
	}}}}
	got := differences(diff)
	if len(got) != 6 || got[0] != "slot 1: no module" || !strings.Contains(got[2], "substitute module") || !strings.Contains(got[5], "substitute submodule") {
		t.Fatalf("differences %q", got)
	}
}

func TestOSBackend(t *testing.T) {
	var b osBackend
	if b.Name() == "" {
		t.Fatal("no backend name")
	}
	if _, err := b.Interfaces(); err != nil {
		t.Fatal(err)
	}
	// Opening an interface that does not exist fails before it needs
	// privileges.
	if _, err := b.Open("profinetctl-none0", nil); err == nil {
		t.Fatal("opened a missing interface")
	}
	a := &app{backend: b, iface: "profinetctl-none0", stderr: &bytes.Buffer{}}
	if _, err := a.openLink(); err == nil {
		t.Fatal("openLink of a missing interface")
	}
	for err, code := range map[error]string{syscall.EPERM: errs.CodePermissionDenied, syscall.EACCES: errs.CodePermissionDenied, os.ErrPermission: errs.CodePermissionDenied} {
		if e, _ := errs.As(openError("x", "y", err)); e.Code != code || e.Kind != errs.KindPermission {
			t.Errorf("%v: %+v", err, e)
		}
	}
	// Execute runs with the arguments of the process.
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"profinetctl", "version"}
	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	code := Execute()
	_ = w.Close()
	os.Stdout = stdout
	var out bytes.Buffer
	_, _ = out.ReadFrom(r)
	if code != 0 || !strings.Contains(out.String(), "profinetctl version") {
		t.Fatalf("execute %d %q", code, out.String())
	}
}

func TestLink(t *testing.T) {
	t.Parallel()
	n := vnet.New(1)
	defer func() { _ = n.Close() }()
	ep, err := n.NewEndpoint(vnet.EndpointConfig{MAC: ethernet.MAC{2, 0, 0, 0, 0, 1}, FilterMulticast: true})
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	l := NewLink(ep, nil, func() { closed = true })
	if l.MAC() != ep.HardwareAddr() {
		t.Fatal("mac")
	}
	// Without a ListenPacket the operating system's UDP stack is used.
	c, err := l.Listen(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	l.Close()
	if !closed {
		t.Fatal("close func not called")
	}
	(&Link{}).Close()
	// The tap passes multicast groups to the inner port.
	var buf bytes.Buffer
	w, _ := pcap.NewWriter(&buf)
	tp := &tappedPort{Port: ep, w: w}
	if err := tp.JoinMulticast(ethernet.LLDPMulticast); err != nil {
		t.Fatal(err)
	}
	if err := tp.LeaveMulticast(ethernet.LLDPMulticast); err != nil {
		t.Fatal(err)
	}
	other := &tappedPort{Port: plainPort{ep}, w: w}
	if err := other.LeaveMulticast(ethernet.LLDPMulticast); err != nil {
		t.Fatal(err)
	}
}

// plainPort hides the optional interfaces of a port.
type plainPort struct{ l2.Port }

func TestHelpers(t *testing.T) {
	t.Parallel()
	if hexString(nil) != "-" || hexString([]byte{1}) != "01" {
		t.Error("hexString")
	}
	if b, err := parseHex("0x01:02-03 04"); err != nil || len(b) != 4 {
		t.Errorf("parseHex %x %v", b, err)
	}
	if prefixLen(netip.MustParseAddr("255.0.255.0")) != -1 || prefixLen(netip.MustParseAddr("::1")) != -1 {
		t.Error("prefixLen")
	}
	if formatSuite(dcp.IPSuite{}) != "-" {
		t.Error("formatSuite")
	}
	if ipSetBy(dcp.IPInfoSet|dcp.IPInfoDHCP) != "dhcp" || ipSetBy(0) != "none" {
		t.Error("ipSetBy")
	}
	if okString(false) != "no answer" || addrOr(netip.Addr{}) != "0.0.0.0" {
		t.Error("strings")
	}
	if r := roles(dcp.RoleIOController | dcp.RolePNSupervisor | dcp.RoleIOMultidev); len(r) != 3 {
		t.Errorf("roles %v", r)
	}
	v := viewOf(dcp.Identity{HasOEM: true, OEMVendorID: 1, OEMDeviceID: 2})
	if v.OEMVendorID == nil || len(v.Roles) != 0 {
		t.Errorf("view %+v", v)
	}
	if s, _ := (identifyResult{Device: deviceView{OEMVendorID: v.OEMVendorID, OEMDeviceID: v.OEMDeviceID, IPConflict: true}}).MarshalTextOutput(); !strings.Contains(s, "OEM") || !strings.Contains(s, "IP conflict") {
		t.Errorf("identify text %q", s)
	}
	if s, _ := (ipResult{IPConflict: true}).MarshalTextOutput(); !strings.Contains(s, "CONFLICT") {
		t.Error("ip conflict text")
	}
	if s, _ := (interfacesResult{}).MarshalTextOutput(); !strings.Contains(s, "no Ethernet interfaces") {
		t.Error("no interfaces text")
	}
	if rows := (changeResult{}).TableRows(); rows[0][4] != "-" {
		t.Error("change rows")
	}
	if s, _ := (recordResult{}).MarshalTextOutput(); !strings.Contains(s, "(empty)") {
		t.Error("empty record")
	}
	if blockTypes([]byte{1}) != nil {
		t.Error("blockTypes of garbage")
	}
	if s, _ := (deviceInfoResult{Errors: []string{"x"}}).MarshalTextOutput(); !strings.Contains(s, "error: x") {
		t.Error("info errors")
	}
	if s, _ := (statusResult{Errors: []string{"x"}, Diagnoses: map[string]int{}}).MarshalTextOutput(); !strings.Contains(s, "error: x") {
		t.Error("status errors")
	}
	if s, _ := (summary{Error: "x"}).MarshalTextOutput(); !strings.Contains(s, "summary") {
		t.Error("summary")
	}
	if h := (summary{}).TableHeaders(); len(h) != 3 {
		t.Error("summary headers")
	}
	if s, _ := (arEvent{Time: "t", Event: "state", From: "a", To: "b", Error: "boom"}).MarshalTextOutput(); !strings.Contains(s, ": boom") {
		t.Error("state error text")
	}
	if _, err := splitAddr("a:b", 3); err == nil {
		t.Error("splitAddr")
	}
	if (&target{}).describe() != "MAC " || (&target{}).matches("", dcp.Identity{}) {
		t.Error("target")
	}
}
