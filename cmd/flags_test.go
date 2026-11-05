// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestFlagValidationBeforeSending: bad AR, duration and name flags are
// refused with exit 3 before the interface is opened. The interface eth9
// does not exist, so a check that came after opening it would fail with
// interface_open_failed instead.
func TestFlagValidationBeforeSending(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	mf := modelFile(t)
	conn := []string{"-i", "eth9", "connect", "-s", "dev-a", "--model", mf}
	long := strings.Repeat("a", 241)
	cases := [][]string{
		{"--send-clock", "0"},
		{"--reduction-ratio", "0"},
		{"--watchdog-factor", "0"},
		{"--data-hold-factor", "0"},
		{"--send-clock", "1000", "--reduction-ratio", "1000"},
		{"--rpc-port", "0"},
		{"--rpc-port", "70000"},
		{"--rpc-port", "-1"},
		{"--controller-name", "Bad Name"},
		{"--controller-name", ""},
		{"--connect-timeout", "0s"},
		{"--activity-timeout", "-1s"},
		{"--duration", "-1s"},
		{"--peer-check", "1=Bad Station/port-001"},
		{"--peer-check", "1=switch-1/" + long},
		{"--mrp-role", "client", "--mrp-domain-name", long},
		{"--mrp-domain-name", "ring-1"},
	}
	for _, c := range cases {
		n.fails(3, "invalid_input", append(append([]string{}, conn...), c...)...)
	}
	n.fails(3, "invalid_input", "-i", "eth9", "io", "statistics", "-s", "dev-a", "--model", mf, "--duration", "0s")
	n.fails(3, "invalid_input", "-i", "eth9", "io", "monitor", "-s", "dev-a", "--model", mf, "--interval", "0s")
	n.fails(3, "invalid_input", "-i", "eth9", "io", "status", "-s", "dev-a", "--model", mf, "--settle", "-1s")
	n.fails(3, "invalid_input", "-i", "eth9", "discover", "--wait", "-1s")
	n.fails(3, "invalid_input", "-i", "eth9", "lldp", "neighbors", "--wait", "-2s")
	// 128 × 512 = 65536 is a counter step of 0; 64 × 512 fits.
	n.fails(3, "invalid_input", append(append([]string{}, conn...), "--send-clock", "128", "--reduction-ratio", "512")...)
	// Accepted: the next step, opening eth9, is what fails.
	n.fails(3, "interface_open_failed", append(append([]string{}, conn...), "--send-clock", "64", "--reduction-ratio", "512")...)
}

func TestRunFor(t *testing.T) {
	t.Parallel()
	start := time.Now()
	if runFor(t.Context(), 0) || time.Since(start) > time.Second {
		t.Fatal("runFor 0 waited or reported an interrupt")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if !runFor(ctx, 0) || !runFor(ctx, time.Hour) {
		t.Fatal("cancelled context not reported")
	}
	if runFor(t.Context(), 10*time.Millisecond) {
		t.Fatal("timer reported as interrupt")
	}
}

func TestSuiteOfRefusesNonHostAddresses(t *testing.T) {
	t.Parallel()
	good := []struct{ addr, gw string }{
		{"192.168.0.10/24", ""}, {"192.168.0.10", "192.168.0.1"}, {"10.1.2.3/8", "0.0.0.0"},
		{"192.168.0.10/31", ""}, {"192.168.0.10/32", ""}, {"192.168.0.1/30", "192.168.0.2"},
	}
	for _, c := range good {
		if _, err := suiteOf(c.addr, c.gw); err != nil {
			t.Errorf("%s gw %q: %v", c.addr, c.gw, err)
		}
	}
	bad := []struct{ addr, gw string }{
		{"0.0.0.0/24", ""}, {"127.0.0.1/8", ""}, {"224.0.0.1/24", ""}, {"255.255.255.255/24", ""},
		{"192.168.0.0/24", ""}, {"192.168.0.255/24", ""}, {"192.168.0.10/0", ""},
		{"192.168.0.10/24", "10.0.0.1"}, {"192.168.0.10/24", "192.168.0.10"}, {"192.168.0.10/24", "192.168.0.255"},
		{"192.168.0.10/24", "fe80::1"}, {"fe80::1/64", ""}, {"nope", ""},
	}
	for _, c := range bad {
		if _, err := suiteOf(c.addr, c.gw); err == nil {
			t.Errorf("%s gw %q accepted", c.addr, c.gw)
		}
	}
}

// TestConflictingFilters: flags that exclude each other are refused
// instead of one being silently ignored.
func TestConflictingFilters(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	n.fails(2, "invalid_flag_combination", "-i", "pn0", "discover", "--station", "dev-a", "--ip", "192.168.0.10")
	n.fails(2, "invalid_flag_combination", "-i", "pn0", "discover", "--alias", "x", "--vendor", "0x2A", "--device", "1")
	n.fails(2, "invalid_flag_combination", "-i", "pn0", "ip", "set", "-s", "dev-a", "--clear", "--gateway", "192.168.0.1")
	n.fails(3, "invalid_input", "-i", "pn0", "ip", "set", "-s", "dev-a", "224.0.0.1/24")
	n.fails(3, "invalid_input", "-i", "pn0", "ip", "set", "-s", "dev-a", "192.168.0.20/24", "--gateway", "10.0.0.1")
	// --clear still works.
	n.ok("-i", "pn0", "ip", "set", "-s", "dev-a", "--clear")
	n.fails(3, "invalid_input", "-i", "pn0", "topology", "check", "--expect", writeFile(t, "empty.json", []byte(`[{}]`)))
	n.fails(3, "invalid_input", "-i", "pn0", "topology", "check", "--expect", writeFile(t, "bad.json", []byte(`[{"a":{"station":"Bad Name","port":"port-001"},"b":{"station":"dev-a","port":"port-001"}}]`)))
	n.fails(3, "invalid_input", "-i", "pn0", "topology", "check", "--link", "Bad Name/port-001=dev-a/port-001")
}
