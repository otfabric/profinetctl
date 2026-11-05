// SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/otfabric/profinetctl/internal/errs"
)

// annotationPositive marks a duration flag that must be above zero (a
// period, a run time or a timeout without a "0: ..." meaning).
const annotationPositive = "profinetctl/positive"

// markPositive marks duration flags of c that must be above zero.
func markPositive(c *cobra.Command, names ...string) {
	for _, n := range names {
		if err := c.Flags().SetAnnotation(n, annotationPositive, []string{"true"}); err != nil {
			panic(err)
		}
	}
}

// checkDurations refuses a negative value of any duration flag of the
// command, and zero for the flags marked positive, before anything is
// sent: a negative --wait or --duration would otherwise mean "no time" or
// "forever" depending on the command.
func checkDurations(c *cobra.Command) error {
	var bad error
	c.Flags().VisitAll(func(f *pflag.Flag) {
		if bad != nil || f.Value.Type() != "duration" {
			return
		}
		// The String of a duration flag is time.Duration.String, which
		// always parses.
		d, _ := time.ParseDuration(f.Value.String())
		_, positive := f.Annotations[annotationPositive]
		switch {
		case d < 0:
			bad = errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--%s %s: must not be negative", f.Name, d), nil)
		case d == 0 && positive:
			bad = errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--%s must be positive", f.Name), nil)
		}
	})
	return bad
}
