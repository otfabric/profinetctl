// SPDX-License-Identifier: MIT

package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// UnknownCommand is the error for the unknown subcommand name of the group cmd, with
// Cobra's suggestions for a misspelt one. The groups' Args and the root's handling of a
// flag after an unknown subcommand both use it, so the message is the same either way.
func UnknownCommand(cmd *cobra.Command, name string) error {
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2 // Cobra's default, which only its root sets
	}
	msg := fmt.Sprintf("unknown command %q for %q", name, cmd.CommandPath())
	if s := cmd.SuggestionsFor(name); len(s) > 0 {
		msg += "\n\nDid you mean this?\n\t" + strings.Join(s, "\n\t")
	}
	return errors.New(msg)
}

// GroupArgs refuses arguments to a group command (client, server): the first one is an
// unknown subcommand.
func GroupArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return UnknownCommand(cmd, args[0])
	}
	return nil
}
