// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/otfabric/go-profinet"
	"github.com/otfabric/go-profinet/controller"
	"github.com/otfabric/go-profinet/ethernet"
	"github.com/otfabric/go-profinet/l2"
	"github.com/otfabric/go-profinet/l2/osport"
	"github.com/otfabric/go-profinet/pcap"

	"github.com/otfabric/profinetctl/internal/errs"
	"github.com/otfabric/profinetctl/internal/format"
)

// Backend opens the network: the interfaces of the host and a Layer 2 link
// on one of them. The real one uses the operating system (raw Ethernet
// sockets and its UDP stack); the tests use a virtual network.
type Backend interface {
	// Name names the Layer 2 backend (afpacket, bpf, npcap).
	Name() string
	Interfaces() ([]osport.Interface, error)
	// Open opens a link on iface; wrap, when not nil, wraps the port
	// before the link's mux reads from it (the --pcap tap).
	Open(iface string, wrap func(l2.Port) l2.Port) (*Link, error)
}

// Link is an open interface: its Layer 2 mux and the UDP sockets of its
// host, for DCE/RPC.
type Link struct {
	Mux *l2.Mux
	// ListenPacket opens UDP sockets; nil uses the operating system's
	// stack.
	ListenPacket controller.ListenPacketFunc
	close        func()
}

// MAC returns the MAC address of the link.
func (l *Link) MAC() ethernet.MAC { return l.Mux.HardwareAddr() }

// Listen opens a UDP socket on the link's host.
func (l *Link) Listen(ctx context.Context, addr string) (net.PacketConn, error) {
	if l.ListenPacket != nil {
		return l.ListenPacket(ctx, "udp4", addr)
	}
	var lc net.ListenConfig
	return lc.ListenPacket(ctx, "udp4", addr)
}

// Close releases the link.
func (l *Link) Close() {
	if l.close != nil {
		l.close()
	}
}

// NewLink wraps a port into a link; close runs after the mux is closed.
// Backends of tests use it.
func NewLink(p l2.Port, listen controller.ListenPacketFunc, close func()) *Link {
	mux := l2.NewMux(p)
	return &Link{Mux: mux, ListenPacket: listen, close: func() {
		_ = mux.Close()
		if close != nil {
			close()
		}
	}}
}

// osBackend is the backend of the operating system.
type osBackend struct{}

func (osBackend) Name() string                            { return osport.Backend() }
func (osBackend) Interfaces() ([]osport.Interface, error) { return osport.Interfaces() }

func (osBackend) Open(iface string, wrap func(l2.Port) l2.Port) (*Link, error) {
	p, err := osport.Open(iface, osport.Options{})
	if err != nil {
		return nil, err
	}
	port := p
	if wrap != nil {
		port = wrap(p)
	}
	return NewLink(port, nil, func() { _ = p.Close() }), nil
}

// openLink opens the interface of the invocation, with the --pcap tap.
func (a *app) openLink() (*Link, error) {
	if a.iface == "" {
		return nil, errs.New(errs.KindUsage, errs.CodeInvalidInput, "no interface: give --interface (or set PROFINETCTL_INTERFACE); profinetctl interfaces lists them", nil)
	}
	if a.pcapFile == "" {
		l, err := a.backend.Open(a.iface, nil)
		if err != nil {
			return nil, openError(a.iface, a.backend.Name(), err)
		}
		return l, nil
	}
	// The file is opened (created) first, so a bad path fails before the
	// interface is touched, but only emptied once the interface is open:
	// a failed open leaves an existing capture alone. It may be a FIFO
	// (live Wireshark) or a device, which are not truncated.
	_, statErr := os.Lstat(a.pcapFile)
	created := errors.Is(statErr, os.ErrNotExist)
	f, err := openOutputFile(a.pcapFile)
	if err != nil {
		return nil, errs.New(errs.KindInvalidInput, errs.CodeOutputFileCreateFailed, fmt.Sprintf("pcap file: %v", err), err)
	}
	tap := &pcapTap{}
	l, err := a.backend.Open(a.iface, func(p l2.Port) l2.Port { return &tappedPort{Port: p, tap: tap} })
	if err != nil {
		_ = f.Close()
		if created {
			_ = os.Remove(a.pcapFile)
		}
		return nil, openError(a.iface, a.backend.Name(), err)
	}
	w, err := beginCapture(f)
	if err != nil {
		l.close()
		_ = f.Close()
		return nil, errs.Output(errs.CodeOutputFileWriteFailed, fmt.Errorf("pcap file: %w", err))
	}
	tap.start(w)
	inner := l.close
	l.close = func() {
		inner()
		endCapture(a.stderr, a.pcapFile, tap, f)
	}
	return l, nil
}

// beginCapture empties f when it is a regular file (not a FIFO or a
// device) and writes the pcap file header.
func beginCapture(f *os.File) (*pcap.Writer, error) {
	if fi, err := f.Stat(); err == nil && fi.Mode().IsRegular() {
		if err := f.Truncate(0); err != nil {
			return nil, err
		}
	}
	return pcap.NewWriter(f)
}

// endCapture stops the tap, closes f and warns on stderr when the
// capture could not be written completely.
func endCapture(stderr io.Writer, path string, tap *pcapTap, f io.Closer) {
	err := tap.stop()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: pcap file %s is incomplete: %s\n", path, format.SafeText(err.Error()))
	}
}

// pcapTap writes the frames of every tapped port of a link to one pcap
// file; frames before start (the interface opening) are not recorded.
// The first write error stops the capture and is reported at the end.
type pcapTap struct {
	mu  sync.Mutex
	w   *pcap.Writer
	err error
}

func (t *pcapTap) start(w *pcap.Writer) {
	t.mu.Lock()
	t.w = w
	t.mu.Unlock()
}

func (t *pcapTap) record(b []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.w == nil || t.err != nil {
		return
	}
	t.err = t.w.WriteFrame(time.Now(), b)
}

// stop ends the capture and returns its first write error.
func (t *pcapTap) stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.w = nil
	return t.err
}

// openError classifies a failure to open an interface.
func openError(iface, backend string, err error) error {
	msg := fmt.Sprintf("cannot open interface %q (%s): %v", iface, backend, err)
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		return errs.New(errs.KindPermission, errs.CodePermissionDenied, msg+": raw Ethernet access needs root, CAP_NET_RAW (Linux), access to /dev/bpf* (macOS) or Npcap (Windows)", err)
	}
	if errors.Is(err, profinet.ErrNotFound) {
		return errs.New(errs.KindInvalidInput, errs.CodeInterfaceOpenFailed, msg, err)
	}
	return errs.New(errs.KindTransport, errs.CodeInterfaceOpenFailed, msg, err)
}

// tappedPort records every frame sent and received into a pcap file.
type tappedPort struct {
	l2.Port
	tap *pcapTap
}

func (p *tappedPort) record(b []byte) { p.tap.record(b) }

func (p *tappedPort) ReadFrame(ctx context.Context, b []byte) (int, error) {
	n, err := p.Port.ReadFrame(ctx, b)
	if err == nil {
		p.record(b[:n])
	}
	return n, err
}

func (p *tappedPort) WriteFrame(ctx context.Context, b []byte) error {
	err := p.Port.WriteFrame(ctx, b)
	if err == nil {
		p.record(ethernet.Pad(append([]byte(nil), b...), 0))
	}
	return err
}

// JoinMulticast and LeaveMulticast pass the multicast groups to the inner
// port, which the tap would otherwise hide.
func (p *tappedPort) JoinMulticast(group ethernet.MAC) error {
	return l2.JoinMulticast(p.Port, group)
}

func (p *tappedPort) LeaveMulticast(group ethernet.MAC) error {
	if mp, ok := p.Port.(l2.MulticastPort); ok {
		return mp.LeaveMulticast(group)
	}
	return nil
}
