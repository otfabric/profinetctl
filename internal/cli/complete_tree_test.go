// SPDX-License-Identifier: MIT

package cli

import (
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCompleteTree(t *testing.T) {
	root := &cobra.Command{Use: "tool"}
	root.PersistentFlags().String("pcap", "", "capture file")
	root.PersistentFlags().Bool("debug", false, "")
	run := func(*cobra.Command, []string) {}
	cert := &cobra.Command{Use: "cert"}
	inspect := &cobra.Command{Use: "inspect FILE", Run: run}
	gen := &cobra.Command{Use: "generate", Run: run}
	gen.Flags().String("out", "", "directory")
	gen.Flags().String("config", "", "a number here")
	gen.Flags().String("format", "", "text or json")
	_ = gen.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"text", "json"}, cobra.ShellCompDirectiveNoFileComp))
	cert.AddCommand(inspect, gen)
	srv := &cobra.Command{Use: "server", Run: run}
	srv.Flags().String("config", "", "seed file")
	srv.Flags().String("log", "", "any file")
	srv.Flags().String("listen", "", "address")
	shells := &cobra.Command{Use: "completion", ValidArgs: []string{"bash"}, Run: run}
	root.AddCommand(cert, srv, shells)

	CompleteTree(root, Completion{
		Files: map[string][]string{"pcap": {"pcap"}, "server --config": {"yaml"}, "log": {}},
		Dirs:  []string{"out"},
		Args:  map[string][]string{"cert inspect": {"pem"}},
	})

	flag := func(c *cobra.Command, name string) string {
		t.Helper()
		fn, ok := c.GetFlagCompletionFunc(name)
		if !ok {
			return "none"
		}
		values, dir := fn(c, nil, "")
		return strings.Join(append(values, ":"+strconv.Itoa(int(dir))), " ")
	}
	for _, c := range []struct {
		cmd        *cobra.Command
		flag, want string
	}{
		{root, "pcap", "pcap :8"},
		{root, "debug", "none"},
		{gen, "out", ":16"},
		{gen, "config", ":4"},
		{gen, "format", "text json :4"},
		{srv, "config", "yaml :8"},
		{srv, "log", ":0"},
		{srv, "listen", ":4"},
	} {
		if got := flag(c.cmd, c.flag); got != c.want {
			t.Errorf("%s --%s: %q, want %q", c.cmd.Name(), c.flag, got, c.want)
		}
	}
	args := func(c *cobra.Command) string {
		if c.ValidArgsFunction == nil {
			return "none"
		}
		values, dir := c.ValidArgsFunction(c, nil, "")
		return strings.Join(append(values, ":"+strconv.Itoa(int(dir))), " ")
	}
	for c, want := range map[*cobra.Command]string{
		inspect: "pem :8",
		gen:     ":4",
		srv:     ":4",
		cert:    "none", // a group: cobra completes its subcommands
		shells:  "none", // ValidArgs
	} {
		if got := args(c); got != want {
			t.Errorf("%s arguments: %q, want %q", c.Name(), got, want)
		}
	}
}
