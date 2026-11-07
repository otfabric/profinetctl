// SPDX-License-Identifier: MIT

package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Completion says what the flags and arguments without a completion of their own complete
// to; see [CompleteTree].
type Completion struct {
	// Files maps a flag name ("config"), or a command path and a flag name for the flag
	// of one command ("adapter --config"), to the extensions (without the dot) of the
	// files it takes. An empty list completes any file.
	Files map[string][]string
	// Dirs are the names of the flags that take a directory.
	Dirs []string
	// Args maps a command path without the root name ("cert inspect") to the extensions
	// of the files its arguments are. An empty list completes any file.
	Args map[string][]string
}

// CompleteTree registers completion for every command of the tree under root that has
// none yet: a flag of c.Files completes files of its extensions, a flag of c.Dirs
// directories, and every other flag that takes a value (an address, a number, a name)
// nothing, not file names. The arguments of a command of c.Args complete files; those of
// any other command without ValidArgs or ValidArgsFunction complete nothing.
// Call it once the tree is built, after the enum completions.
func CompleteTree(root *cobra.Command, c Completion) {
	dirs := map[string]bool{}
	for _, d := range c.Dirs {
		dirs[d] = true
	}
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		path := strings.TrimSpace(strings.TrimPrefix(cmd.CommandPath(), root.Name()))
		register := func(f *pflag.Flag) {
			if f.NoOptDefVal != "" { // a bool (or a flag with an optional value): nothing to complete
				return
			}
			if _, ok := cmd.GetFlagCompletionFunc(f.Name); ok {
				return
			}
			fn := cobra.NoFileCompletions
			if exts, ok := c.Files[strings.TrimSpace(path+" --"+f.Name)]; ok {
				fn = fileCompletion(exts)
			} else if exts, ok := c.Files[f.Name]; ok {
				fn = fileCompletion(exts)
			} else if dirs[f.Name] {
				fn = func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
					return nil, cobra.ShellCompDirectiveFilterDirs
				}
			}
			_ = cmd.RegisterFlagCompletionFunc(f.Name, fn)
		}
		cmd.LocalNonPersistentFlags().VisitAll(register)
		cmd.PersistentFlags().VisitAll(register)
		if len(cmd.ValidArgs) == 0 && cmd.ValidArgsFunction == nil {
			if exts, ok := c.Args[path]; ok {
				cmd.ValidArgsFunction = fileCompletion(exts)
			} else if cmd.Runnable() || !cmd.HasSubCommands() { // cobra completes the subcommands of a group
				cmd.ValidArgsFunction = cobra.NoFileCompletions
			}
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

// fileCompletion completes files of the extensions exts, or any file when exts is empty.
func fileCompletion(exts []string) cobra.CompletionFunc {
	return func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		if len(exts) == 0 {
			return nil, cobra.ShellCompDirectiveDefault
		}
		return exts, cobra.ShellCompDirectiveFilterFileExt
	}
}
