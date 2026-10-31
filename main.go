// SPDX-License-Identifier: MIT

// Command profinetctl is a PROFINET IO command-line tool built on
// github.com/otfabric/go-profinet.
package main

import (
	"os"

	"github.com/otfabric/profinetctl/cmd"
)

// Injected at link time (see Makefile): -X main.version=... etc.
var (
	version   = "dev"
	tag       = ""
	commit    = ""
	buildDate = ""
)

// exit ends the process; a test replaces it.
var exit = os.Exit

func main() {
	cmd.SetBuildMeta(version, tag, commit, buildDate)
	exit(cmd.Execute())
}
