// SPDX-License-Identifier: MIT

// Package cmd is the command tree of profinetctl.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	clihelp "github.com/otfabric/profinetctl/internal/cli"
	"github.com/otfabric/profinetctl/internal/errs"
	"github.com/otfabric/profinetctl/internal/format"
	"github.com/otfabric/profinetctl/internal/runner"
)

// buildMeta holds link-time metadata set from main via SetBuildMeta.
var buildMeta struct {
	version, tag, commit, buildDate string
}

// SetBuildMeta is called from main before Execute; values come from ldflags -X main.*.
func SetBuildMeta(version, tag, commit, buildDate string) {
	buildMeta.version = version
	buildMeta.tag = tag
	buildMeta.commit = commit
	buildMeta.buildDate = buildDate
}

// Environment variables that set the defaults of the global flags.
const (
	EnvInterface = "PROFINETCTL_INTERFACE"
	EnvFormat    = "PROFINETCTL_FORMAT"
	EnvTimeout   = "PROFINETCTL_TIMEOUT"
)

// app is the state of one invocation: the global flags and the backend.
type app struct {
	backend  Backend
	iface    string
	format   string
	timeout  time.Duration
	debug    bool
	pcapFile string
	stderr   io.Writer
}

// NewRootCmd builds the command tree on a backend. Every call returns a fresh tree.
func NewRootCmd(b Backend) *cobra.Command {
	a := &app{backend: b, stderr: os.Stderr}
	root := &cobra.Command{
		Use:   "profinetctl",
		Short: "PROFINET IO command-line tool: discovery, configuration, records, diagnostics and cyclic IO",
		Long: `profinetctl speaks PROFINET IO (IEC 61158-6-10) on an Ethernet interface.

It discovers devices and sets their station names and IP addresses with DCP, reads
their identification (I&M), records and diagnosis over DCE/RPC, inspects, validates
and generates GSDML files, lists LLDP neighbors and the topology, and establishes an
application relationship (AR) as IO-Controller to monitor cyclic IO data and alarms.

Commands that change a device (name set, ip set, reset, record write, connect with
outputs) only do what the command line says. reset requires --confirm with the
target's station name or MAC address.

Results go to stdout as text, a table or JSON (--format); diagnostics go to stderr.
Raw Ethernet access needs privileges: root or CAP_NET_RAW on Linux, access to
/dev/bpf* on macOS, Npcap on Windows. Exit codes are stable: see docs/exitcodes.md.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(c *cobra.Command, _ []string) error {
			a.stderr = c.ErrOrStderr()
			if _, err := runner.Output(c.Context(), a.format); err != nil {
				return err
			}
			if a.timeout <= 0 {
				return errs.InvalidInput(errs.CodeInvalidInput, "--timeout must be positive", nil)
			}
			return nil
		},
	}
	pf := root.PersistentFlags()
	pf.StringVarP(&a.iface, "interface", "i", os.Getenv(EnvInterface), "Ethernet interface (env "+EnvInterface+")")
	pf.StringVarP(&a.format, "format", "o", envOr(EnvFormat, "text"), "Output format: text, json or table (env "+EnvFormat+")")
	pf.DurationVar(&a.timeout, "timeout", envDuration(EnvTimeout, 5*time.Second), "Time limit of one request to a device (env "+EnvTimeout+")")
	pf.BoolVar(&a.debug, "debug", false, "Log go-profinet's diagnostics to stderr and show the causes of an error")
	pf.StringVar(&a.pcapFile, "pcap", "", "Record every frame sent and received to this pcap file")
	_ = clihelp.RegisterEnumFlagCompletionWithDescriptions(root, "format", format.ValueDescriptions())
	_ = root.RegisterFlagCompletionFunc("interface", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		ifs, _ := b.Interfaces()
		var out []string
		for _, i := range ifs {
			out = append(out, i.Name+"\t"+i.MAC)
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	})

	root.AddCommand(
		newInterfacesCmd(a),
		newDiscoverCmd(a, "discover"),
		newDiscoverCmd(a, "devices"),
		newIdentifyCmd(a),
		newNameCmd(a),
		newIPCmd(a),
		newSignalCmd(a),
		newResetCmd(a),
		newGSDMLCmd(a),
		newRecordCmd(a),
		newIMCmd(a),
		newDiagnosticsCmd(a),
		newDeviceCmd(a),
		newStatusCmd(a),
		newConnectCmd(a),
		newDisconnectCmd(a),
		newARCmd(a),
		newIOCRCmd(a),
		newIOCmd(a),
		newAlarmsCmd(a),
		newLLDPCmd(a),
		newTopologyCmd(a),
		newCompletionCmd(),
		newVersionCmd(),
	)
	return root
}

func envOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

// envDuration returns the duration of an environment variable; a malformed
// one panics, which buildRoot reports as invalid input.
func envDuration(name string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		panic(fmt.Sprintf("profinetctl: %s: %v", name, err))
	}
	return d
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of profinetctl",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			out := cmd.OutOrStdout()
			info, _ := debug.ReadBuildInfo()
			ver, commit := versionOf(buildMeta.version, buildMeta.commit, info)
			if ver == "" {
				ver = "(unknown)"
			}
			_, _ = fmt.Fprintf(out, "profinetctl version: %s\n", ver)
			if s := strings.TrimSpace(buildMeta.tag); s != "" {
				_, _ = fmt.Fprintf(out, "tag:         %s\n", s)
			}
			if commit != "" {
				_, _ = fmt.Fprintf(out, "commit:      %s\n", commit)
			}
			if s := strings.TrimSpace(buildMeta.buildDate); s != "" {
				_, _ = fmt.Fprintf(out, "build date:  %s\n", s)
			}
			if info != nil {
				for _, d := range info.Deps {
					if d.Path == "github.com/otfabric/go-profinet" {
						_, _ = fmt.Fprintf(out, "go-profinet: %s\n", d.Version)
					}
				}
			}
			_, _ = fmt.Fprintf(out, "go version:  %s\n", runtime.Version())
			_, _ = fmt.Fprintf(out, "platform:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
		},
	}
}

// devVersion is the version of a binary built without the ldflags of the Makefile.
const devVersion = "dev"

// versionOf returns the version and commit to report: the ldflags of the Makefile and
// of the release set them; a binary built without them reports what the go toolchain
// records (the module version and the VCS revision).
func versionOf(version, commit string, info *debug.BuildInfo) (string, string) {
	version, commit = strings.TrimSpace(version), strings.TrimSpace(commit)
	if info == nil {
		return version, commit
	}
	if version == devVersion || version == "" {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			version = v
		}
	}
	if commit == "" {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				commit = s.Value
				if len(commit) > 7 {
					commit = commit[:7]
				}
			}
		}
	}
	return version, commit
}

// Execute runs the root command and returns the process exit code (main should call os.Exit).
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Run(ctx, osBackend{}, os.Args[1:], os.Stdout, os.Stderr)
}

// Run executes profinetctl with args on a backend, writing to stdout and stderr, and
// returns the exit code. It is Execute without the process: what the tests drive.
func Run(ctx context.Context, b Backend, args []string, stdout, stderr io.Writer) int {
	root, err := buildRoot(b)
	if err != nil {
		return runner.RenderFatal(nil, err, stdout, stderr, false)
	}
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	inv := &runner.Invocation{}
	ctx = runner.WithInvocation(ctx, inv)
	cmd, err := root.ExecuteContextC(ctx)
	trace, _ := root.PersistentFlags().GetBool("debug")
	if err != nil {
		if !inv.HasOutputFormat {
			// Cobra failed before the command recorded its format: honour --format
			// (or PROFINETCTL_FORMAT) all the same.
			raw, ok := runner.FormatArg(args)
			if !ok {
				raw = envOr(EnvFormat, "text")
			}
			if f, ferr := format.Parse(raw); ferr == nil {
				inv.SetOutputFormat(f)
			}
		}
		return runner.RenderFatal(inv, usageError(unknownSubcommand(cmd, args, err)), stdout, stderr, trace)
	}
	if inv.SuccessExitSet {
		return inv.SuccessExitCode
	}
	return errs.ExitOK
}

// buildRoot is NewRootCmd with a malformed environment variable reported as invalid
// input instead of a panic.
func buildRoot(b Backend) (root *cobra.Command, err error) {
	defer func() {
		if r := recover(); r != nil {
			msg := strings.TrimPrefix(fmt.Sprint(r), "profinetctl: ")
			err = errs.InvalidInput(errs.CodeInvalidInput, msg, nil)
		}
	}()
	return NewRootCmd(b), nil
}

// unknownSubcommand returns the error for an unknown subcommand of a group command in
// place of Cobra's error about the first flag (Cobra stops at the group, which has
// none of its subcommands' flags).
func unknownSubcommand(cmd *cobra.Command, args []string, err error) error {
	if cmd == nil || !cmd.HasSubCommands() || !strings.HasPrefix(err.Error(), "unknown flag") {
		return err
	}
	depth := len(strings.Fields(cmd.CommandPath())) - 1
	words := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "-") {
			if f := lookupFlag(cmd, a); f {
				i++
			}
			continue
		}
		words++
		if words <= depth {
			continue
		}
		for _, sub := range cmd.Commands() {
			if sub.Name() == a || sub.HasAlias(a) {
				return err
			}
		}
		return clihelp.UnknownCommand(cmd, a)
	}
	return err
}

// lookupFlag reports whether the flag arg (without "=") of cmd, its subcommands or the
// root takes a value, so that the next argument is that value.
func lookupFlag(cmd *cobra.Command, arg string) bool {
	if strings.Contains(arg, "=") {
		return false
	}
	name := strings.TrimLeft(arg, "-")
	long := strings.HasPrefix(arg, "--")
	if !long && len(name) != 1 {
		return false
	}
	for _, c := range append([]*cobra.Command{cmd, cmd.Root()}, cmd.Commands()...) {
		f := c.Flags().Lookup(name)
		if !long {
			f = c.Flags().ShorthandLookup(name)
		}
		if f == nil {
			if long {
				f = c.PersistentFlags().Lookup(name)
			} else {
				f = c.PersistentFlags().ShorthandLookup(name)
			}
		}
		if f != nil {
			return f.NoOptDefVal == ""
		}
	}
	return false
}

// usageError classifies what Cobra itself rejects (an unknown command or flag, a
// missing required flag, a value a flag cannot hold) as a usage error: exit code 2.
func usageError(err error) error {
	if _, ok := errs.As(err); ok {
		return err
	}
	return errs.New(errs.KindUsage, errs.CodeInvalidInput, err.Error(), err)
}
