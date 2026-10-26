// SPDX-License-Identifier: MIT

package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCompletion(t *testing.T) {
	c := &cobra.Command{Use: "x"}
	c.Flags().String("mode", "", "")
	c.Flags().String("format", "", "")
	if err := RegisterEnumFlagCompletion(c, "mode", []string{"Alpha", "beta"}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterEnumFlagCompletionWithDescriptions(c, "format", map[string]string{"json": "machine", "text": ""}); err != nil {
		t.Fatal(err)
	}
	fn, _ := c.GetFlagCompletionFunc("mode")
	if got, _ := fn(c, nil, "al"); len(got) != 1 || got[0] != "Alpha" {
		t.Errorf("mode %v", got)
	}
	if got, _ := fn(c, nil, ""); len(got) != 2 {
		t.Errorf("mode all %v", got)
	}
	fn, _ = c.GetFlagCompletionFunc("format")
	if got, _ := fn(c, nil, ""); len(got) != 2 || got[0] != "json\tmachine" || got[1] != "text" {
		t.Errorf("format %v", got)
	}
	if got, _ := fn(c, nil, "t"); len(got) != 1 {
		t.Errorf("format t %v", got)
	}
}

func TestUnknownCommand(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	g := &cobra.Command{Use: "name", Args: GroupArgs, RunE: func(*cobra.Command, []string) error { return nil }}
	g.AddCommand(&cobra.Command{Use: "get", Run: func(*cobra.Command, []string) {}})
	root.AddCommand(g)
	if err := GroupArgs(g, nil); err != nil {
		t.Fatal(err)
	}
	err := GroupArgs(g, []string{"gte"})
	if err == nil || !strings.Contains(err.Error(), `unknown command "gte" for "root name"`) || !strings.Contains(err.Error(), "Did you mean this?\n\tget") {
		t.Fatalf("err %v", err)
	}
	MustMarkFlagRequired(withFlag(), "f")
	defer func() {
		if recover() == nil {
			t.Fatal("no panic for a missing flag")
		}
	}()
	MustMarkFlagRequired(&cobra.Command{Use: "y"}, "nope")
}

func withFlag() *cobra.Command {
	c := &cobra.Command{Use: "z"}
	c.Flags().String("f", "", "")
	return c
}
