// SPDX-License-Identifier: MIT

//go:build e2e && !windows

package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// interrupt runs lldp neighbors, which listens on the interface and sends
// nothing, for an hour. It sends sig once the binary has opened the
// interface, which the binary shows by writing the pcap file header to the
// FIFO --pcap names, and returns the exit code and stdout.
func interrupt(t *testing.T, iface string, sig os.Signal) (int, string) {
	t.Helper()
	bin := os.Getenv("PROFINETCTL_E2E_BIN")
	if bin == "" {
		t.Fatal("PROFINETCTL_E2E_BIN is not set; run make e2e")
	}
	fifo := filepath.Join(t.TempDir(), "capture.pcap")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	cmd := exec.Command(bin, "lldp", "neighbors", "-i", iface, "--wait", "1h", "--pcap", fifo, "--format", "json")
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	end := func() error {
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			return fmt.Errorf("killed after 10 s (%v)", <-done)
		}
	}
	opened := make(chan error, 1)
	go func() {
		f, err := os.Open(fifo)
		if err != nil {
			opened <- err
			return
		}
		defer func() { _ = f.Close() }()
		var header [24]byte
		_, err = io.ReadFull(f, header[:])
		opened <- err
		_, _ = io.Copy(io.Discard, f)
	}()
	select {
	case err := <-opened:
		if err != nil {
			err := end()
			t.Fatalf("lldp neighbors did not open %s (%v)\nstdout: %s\nstderr: %s", iface, err, out.String(), errOut.String())
		}
	case err := <-done:
		t.Fatalf("lldp neighbors ended before it opened %s (%v)\nstdout: %s\nstderr: %s", iface, err, out.String(), errOut.String())
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("lldp neighbors did not open %s within 30 s (killed: %v)", iface, <-done)
	}
	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	err := end()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, out.String()
	case errors.As(err, &exit):
		return exit.ExitCode(), out.String()
	default:
		t.Fatalf("lldp neighbors did not end on %v: %v", sig, err)
		return -1, ""
	}
}

// Ctrl-C (SIGINT) and SIGTERM end a one-shot command that has no result yet
// with exit code 130 and one JSON error of kind interrupted
// (docs/exitcodes.md). PROFINETCTL_E2E_INTERFACE names an Ethernet interface
// the binary can open; the CI job sets it (a veth pair, CAP_NET_RAW on the
// binary), and the test is skipped without it.
func TestInterruptExits130(t *testing.T) {
	iface := os.Getenv("PROFINETCTL_E2E_INTERFACE")
	if iface == "" {
		t.Skip("PROFINETCTL_E2E_INTERFACE is not set: no interface to listen on")
	}
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		code, out := interrupt(t, iface, sig)
		if code != 130 || !strings.Contains(out, `"kind": "interrupted"`) {
			t.Fatalf("%v: exit %d, want 130\n%s", sig, code, out)
		}
	}
}
