// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/otfabric/go-profinet/pcap"
	"github.com/otfabric/go-profinet/pnio"
)

func TestReadLimited(t *testing.T) {
	t.Parallel()
	if b, err := readLimited(bytes.NewReader(make([]byte, 8)), 8); err != nil || len(b) != 8 {
		t.Fatalf("at the limit: %d %v", len(b), err)
	}
	if _, err := readLimited(bytes.NewReader(make([]byte, 9)), 8); err == nil || !strings.Contains(err.Error(), "larger than 8 octets") {
		t.Fatalf("over the limit: %v", err)
	}
	boom := errors.New("boom")
	if _, err := readLimited(failingReader{boom}, 8); !errors.Is(err, boom) {
		t.Fatalf("read error: %v", err)
	}
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

// TestInputFileTooLarge: a model, record data or expected topology file
// above maxInputFile is refused before it is read whole.
func TestInputFileTooLarge(t *testing.T) {
	t.Parallel()
	big := filepath.Join(t.TempDir(), "big")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxInputFile + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := readInputFile(big); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("read: %v", err)
	}
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	n.fails(3, "input_file_open_failed", "gsdml", "generate", "--model", big)
	n.fails(3, "input_file_open_failed", "-i", "pn0", "record", "write", "-s", "dev-a", "--index", "1", "--data-file", big)
	n.fails(3, "invalid_input", "-i", "pn0", "topology", "check", "--expect", big)
}

// TestOutputFileMode: generated GSDML, extracted parts and captures are
// readable by the operator only.
func TestOutputFileMode(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	dir := t.TempDir()
	gen := filepath.Join(dir, "dev.xml")
	n.ok("gsdml", "generate", "--model", modelFile(t), "-O", gen)
	part := filepath.Join(dir, "part.xml")
	n.ok("gsdml", "gsdx", gsdxFile(t, n, false), "--extract", "/GSDML-V2.35-OTF-TEST-20261027.xml", "-O", part)
	capture := filepath.Join(dir, "x.pcap")
	n.ok("-i", "pn0", "--pcap", capture, "discover", "--wait", "100ms")
	for _, p := range []string{gen, part, capture} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if m := fi.Mode().Perm(); m&0o077 != 0 {
			t.Errorf("%s: mode %v", filepath.Base(p), m)
		}
	}
	// An existing capture is truncated, not appended to.
	if err := os.WriteFile(capture, bytes.Repeat([]byte{1}, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	n.ok("-i", "pn0", "--pcap", capture, "discover", "--wait", "100ms")
	if fi, _ := os.Stat(capture); fi.Size() >= 1<<20 {
		t.Fatalf("capture not truncated: %d", fi.Size())
	}
	// Extracting to a path that cannot be written is an output error.
	n.fails(10, "output_file_write_failed", "gsdml", "gsdx", gsdxFile(t, n, false), "--extract", "/GSDML-V2.35-OTF-TEST-20261027.xml", "-O", filepath.Join(dir, "no", "x.xml"))
}

func TestIMRecord(t *testing.T) {
	t.Parallel()
	for n := range 5 {
		if idx, ok := imRecord(n); !ok || idx != pnio.IndexIM0+pnio.Index(n) {
			t.Fatalf("I&M%d: %v %v", n, idx, ok)
		}
	}
	for _, n := range []int{-1, 5, 1 << 16} {
		if _, ok := imRecord(n); ok {
			t.Fatalf("I&M%d accepted", n)
		}
	}
	if e := readIM(t.Context(), nil, 0, 1, 5); e.Error == "" || e.Record != "I&M5" {
		t.Fatalf("readIM 5: %+v", e)
	}
}

// TestPcapFileKeptOnFailedOpen: an interface that cannot be opened
// leaves an existing capture alone and creates no new file.
func TestPcapFileKeptOnFailedOpen(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	dir := t.TempDir()
	old := filepath.Join(dir, "old.pcap")
	if err := os.WriteFile(old, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	n.fails(3, "interface_open_failed", "-i", "eth9", "--pcap", old, "discover")
	if b, _ := os.ReadFile(old); string(b) != "keep me" {
		t.Fatalf("existing capture changed: %q", b)
	}
	fresh := filepath.Join(dir, "fresh.pcap")
	n.fails(3, "interface_open_failed", "-i", "eth9", "--pcap", fresh, "discover")
	if _, err := os.Stat(fresh); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file left behind: %v", err)
	}
}

// TestPcapTapWriteError: a failed write stops the capture and is
// reported once, at the end.
func TestPcapTapWriteError(t *testing.T) {
	t.Parallel()
	fw := &failAfter{n: 1}
	w, err := pcap.NewWriter(fw)
	if err != nil {
		t.Fatal(err)
	}
	tap := &pcapTap{}
	tap.record([]byte{1}) // before start: not recorded
	if fw.writes != 1 {
		t.Fatalf("recorded before start: %d writes", fw.writes)
	}
	tap.start(w)
	tap.record(make([]byte, 60))
	tap.record(make([]byte, 60))
	if err := tap.stop(); err == nil || fw.writes != 2 {
		t.Fatalf("stop: %v after %d writes", err, fw.writes)
	}
	tap.record(make([]byte, 60))
	if fw.writes != 2 {
		t.Fatal("recorded after stop")
	}
}

// failAfter accepts n writes, then fails.
type failAfter struct{ n, writes int }

func (f *failAfter) Write(b []byte) (int, error) {
	f.writes++
	if f.writes > f.n {
		return 0, errors.New("disk full")
	}
	return len(b), nil
}
