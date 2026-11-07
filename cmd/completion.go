// SPDX-License-Identifier: MIT

package cmd

import (
	"github.com/spf13/cobra"

	clihelp "github.com/otfabric/profinetctl/internal/cli"
)

const (
	completionBash       = "bash"
	completionZsh        = "zsh"
	completionFish       = "fish"
	completionPowerShell = "powershell"
)

var completionShells = []string{completionBash, completionZsh, completionFish, completionPowerShell}

func newCompletionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: `Output a completion script for the given shell.

  Bash:        source <(profinetctl completion bash)
  Zsh:         source <(profinetctl completion zsh)
  Fish:        profinetctl completion fish | source
  PowerShell:  profinetctl completion powershell | Out-String | Invoke-Expression

Flags with a fixed set of values (--format, --mode, --mrp-role, --record) complete
to those values, --interface to the Ethernet interfaces; GSDML, GSDX, model, expected
link and pcap files complete by type.`,
		ValidArgs: completionShells,
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, out := cmd.Root(), cmd.OutOrStdout()
			switch args[0] {
			case completionBash:
				return root.GenBashCompletionV2(out, true)
			case completionZsh:
				return root.GenZshCompletion(out)
			case completionFish:
				return root.GenFishCompletion(out, true)
			default:
				return root.GenPowerShellCompletion(out)
			}
		},
	}
}

// registerCompletions completes what no command completes itself: --mrp-role and the
// I&M records, the flags and arguments that take a file (files of its type), and every
// other flag that takes a value (an address, a name, a number) and every other argument
// nothing.
func registerCompletions(root *cobra.Command) {
	mrpRoles, imRecords := []string{"client", "manager", "off"}, []string{"0", "1", "2", "3", "4"}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Flags().Lookup("mrp-role") != nil {
			_ = clihelp.RegisterEnumFlagCompletion(c, "mrp-role", mrpRoles)
		}
		if c.Name() == "read" && c.Parent() != nil && c.Parent().Name() == "im" {
			_ = clihelp.RegisterEnumFlagCompletion(c, "record", imRecords)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	gsdml, model := []string{"xml"}, []string{"json"}
	clihelp.CompleteTree(root, clihelp.Completion{
		Files: map[string][]string{
			"pcap": {"pcap"}, "gsdml": gsdml, "model": model, "expect": model,
			"data-file": {}, "output-file": {},
		},
		Args: map[string][]string{
			"gsdml inspect": gsdml, "gsdml validate": gsdml, "gsdml check": gsdml,
			"gsdml modules": gsdml, "gsdml submodules": gsdml, "gsdml gsdx": {"gsdx"},
		},
	})
}
