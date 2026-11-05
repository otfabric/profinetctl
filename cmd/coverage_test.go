// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/otfabric/go-profinet/device"
	"github.com/otfabric/go-profinet/pcap"
	"github.com/otfabric/go-profinet/pnio"
)

func TestMarkPositiveUnknownFlag(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("no panic for a flag that does not exist")
		}
	}()
	markPositive(&cobra.Command{}, "nope")
}

// TestRingPorts: absent port submodules are left out, failed reads and
// undecodable records are listed, the rest are ring ports.
func TestRingPorts(t *testing.T) {
	t.Parallel()
	good := (&pnio.PDPortMrpData{Real: true, Domain: [16]byte{1}}).Append(nil)
	blk, _, err := pnio.ReadBlock(good)
	if err != nil {
		t.Fatal(err)
	}
	bad := blk
	bad.Body = bad.Body[:3]
	ports, failed := ringPorts(4, func(sub uint16) (pnio.RawBlock, bool, error) {
		switch sub {
		case 0x8001:
			return blk, true, nil
		case 0x8002:
			return pnio.RawBlock{}, false, errors.New("timeout")
		case 0x8003:
			return bad, true, nil
		default:
			return pnio.RawBlock{}, false, nil
		}
	})
	if len(ports) != 1 || ports[0].Subslot != "0x8001" || !strings.HasPrefix(ports[0].Domain, "01000000-") {
		t.Fatalf("ports %+v", ports)
	}
	if len(failed) != 2 || !strings.HasPrefix(failed[0], "0x8002: timeout") || !strings.HasPrefix(failed[1], "0x8003: ") {
		t.Fatalf("failed %q", failed)
	}
	if ports, failed := ringPorts(255, func(uint16) (pnio.RawBlock, bool, error) { return pnio.RawBlock{}, false, nil }); len(ports) != 0 || ports == nil || failed != nil {
		t.Fatalf("no ports: %v %v", ports, failed)
	}
}

func TestBeginCapture(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "x.pcap")
	if err := os.WriteFile(p, bytes.Repeat([]byte{9}, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := beginCapture(f); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if fi, _ := os.Stat(p); fi.Size() != 24 {
		t.Fatalf("size %d, want the 24-octet header", fi.Size())
	}
	// A read-only file cannot be truncated.
	ro, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }()
	if _, err := beginCapture(ro); err == nil {
		t.Fatal("truncate of a read-only file succeeded")
	}
	// A closed file is not regular to Stat, and the header write fails.
	closed, _ := os.Create(filepath.Join(dir, "c.pcap"))
	_ = closed.Close()
	if _, err := beginCapture(closed); err == nil {
		t.Fatal("header written to a closed file")
	}
}

type closeErr struct{ err error }

func (c closeErr) Close() error { return c.err }

func TestEndCapture(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	endCapture(&b, "ok.pcap", &pcapTap{}, closeErr{})
	if b.Len() != 0 {
		t.Fatalf("warning for a complete capture: %q", b.String())
	}
	endCapture(&b, "c.pcap", &pcapTap{}, closeErr{errors.New("disk full")})
	if !strings.Contains(b.String(), "warning: pcap file c.pcap is incomplete: disk full") {
		t.Fatalf("close error: %q", b.String())
	}
	b.Reset()
	w, _ := pcap.NewWriter(&failAfter{n: 1})
	tap := &pcapTap{}
	tap.start(w)
	tap.record(make([]byte, 60))
	endCapture(&b, "w.pcap", tap, closeErr{errors.New("later")})
	if !strings.Contains(b.String(), "incomplete: disk full\n") {
		t.Fatalf("the first error is reported: %q", b.String())
	}
}

// TestPcapHeaderWriteFails: a capture file that refuses the header is
// an output error, and the interface is closed again (/dev/full, Linux).
func TestPcapHeaderWriteFails(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("no /dev/full")
	}
	n := newTestNet(t)
	n.fails(10, "output_file_write_failed", "-i", "pn0", "--pcap", "/dev/full", "discover", "--wait", "100ms")
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestExtractToBrokenStdout(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	var e bytes.Buffer
	code := Run(t.Context(), n, []string{"gsdml", "gsdx", gsdxFile(t, n, false), "--extract", "/GSDML-V2.35-OTF-TEST-20261027.xml"}, failWriter{}, &e)
	if code != 10 {
		t.Fatalf("exit %d: %s", code, e.String())
	}
}

func TestLinkPortTooLong(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.fails(3, "invalid_input", "-i", "pn0", "topology", "check", "--link", "dev-a/"+strings.Repeat("p", 241)+"=dev-b/port-001")
}

// TestIOStatisticsInterrupted: Ctrl-C before --duration ends is exit 130.
func TestIOStatisticsInterrupted(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	d := n.device("dev-a", "192.168.0.10")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer cancel()
		for range 1000 {
			for _, a := range d.dev.ARs() {
				if a.State == device.ARStateData {
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	var e bytes.Buffer
	code := Run(ctx, n, []string{"-i", "pn0", "io", "statistics", "-s", "dev-a", "--model", modelFile(t), "--duration", "1h"}, io.Discard, &e)
	if code != 130 {
		t.Fatalf("exit %d: %s", code, e.String())
	}
}

func TestReparamAfter(t *testing.T) {
	t.Parallel()
	events := make(chan arEvent, 1)
	called := false
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reparamAfter(ctx, time.Hour, func(context.Context) error { called = true; return nil }, events)
	if called || len(events) != 0 {
		t.Fatal("re-parameterized after the AR ended")
	}
	reparamAfter(t.Context(), time.Millisecond, func(context.Context) error { return context.DeadlineExceeded }, events)
	if e := <-events; e.Event != "reparameterized" || e.Error == "" {
		t.Fatalf("failed reparameterization: %+v", e)
	}
	events <- arEvent{}
	// A full buffer drops the event instead of blocking.
	reparamAfter(t.Context(), time.Millisecond, func(context.Context) error { return nil }, events)
}
