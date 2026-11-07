// SPDX-License-Identifier: MIT

package cmd

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// completeLine runs the hidden __complete command and returns the candidates (descriptions
// cut off) followed by the directive line (":4").
func completeLine(n *testNet, args ...string) string {
	n.t.Helper()
	out := n.ok(append([]string{cobra.ShellCompRequestCmd}, args...)...)
	var words []string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		words = append(words, strings.SplitN(line, "\t", 2)[0])
	}
	return strings.Join(words, " ")
}

func TestCompletionCandidates(t *testing.T) {
	n := newTestNet(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"completion", ""}, "bash zsh fish powershell :4"},
		{[]string{"discover", "--format", ""}, "json table text :4"},
		{[]string{"version", "-o", "j"}, "json :4"},
		{[]string{"connect", "--mrp-role", ""}, "client manager off :4"},
		{[]string{"im", "read", "--record", ""}, "0 1 2 3 4 :4"},
		{[]string{"reset", "--mode", "fac"}, "factory :4"},
		{[]string{"gsdml", "inspect", ""}, "xml :8"},
		{[]string{"gsdml", "gsdx", ""}, "gsdx :8"},
		{[]string{"connect", "--gsdml", ""}, "xml :8"},
		{[]string{"connect", "--model", ""}, "json :8"},
		{[]string{"topology", "check", "--expect", ""}, "json :8"},
		{[]string{"discover", "--pcap", ""}, "pcap :8"},
		{[]string{"record", "write", "--data-file", ""}, ":0"},
		{[]string{"identify", "--station", ""}, ":4"},
		{[]string{"ip", "set", ""}, ":4"},
		{[]string{"connect", "--send-clock", ""}, ":4"},
	} {
		if got := completeLine(n, c.args...); got != c.want {
			t.Errorf("completion of %v: %q, want %q", c.args, got, c.want)
		}
	}
}

// valueSet matches a usage that lists the values of a flag: "text, json or table",
// "a role: client, manager or off", "one of …", "a|b".
var valueSet = regexp.MustCompile(`(^|: )[a-z0-9-]+( \([^)]*\))?(, [a-z0-9-]+( \([^)]*\))?)*,? or [a-z0-9-]+|\bone of\b|\b[a-z0-9-]+\|[a-z0-9-]+\b`)

// Every flag that takes a value completes, and one whose usage lists its values
// completes to them, not to file names.
func TestEveryFlagCompletes(t *testing.T) {
	n := newTestNet(t)
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.NoOptDefVal != "" || f.Name == "help" {
				return
			}
			fn, ok := c.GetFlagCompletionFunc(f.Name)
			if !ok {
				t.Errorf("%s --%s has no completion", c.CommandPath(), f.Name)
				return
			}
			if valueSet.MatchString(f.Usage) || f.Name == "mode" {
				values, dir := fn(c, nil, "")
				if len(values) < 2 || dir != cobra.ShellCompDirectiveNoFileComp {
					t.Errorf("%s --%s (%q) completes %q, directive %d", c.CommandPath(), f.Name, f.Usage, values, dir)
				}
			}
		})
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(NewRootCmd(n))
}
