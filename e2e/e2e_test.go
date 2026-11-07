// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// run executes the binary named by PROFINETCTL_E2E_BIN (set by make e2e).
func run(t *testing.T, args ...string) (output string, exitCode int) {
	t.Helper()
	bin := os.Getenv("PROFINETCTL_E2E_BIN")
	if bin == "" {
		t.Fatal("PROFINETCTL_E2E_BIN is not set; run make e2e")
	}
	var out bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), 0
	case errors.As(err, &exit):
		return out.String(), exit.ExitCode()
	default:
		t.Fatalf("run %s: %v", bin, err)
		return "", -1
	}
}

func TestVersionPrintsTheBuiltVersion(t *testing.T) {
	output, code := run(t, "version")
	if code != 0 {
		t.Fatalf("exit code %d, want 0\n%s", code, output)
	}
	// make e2e sets the variable to the tag of HEAD, empty when HEAD has none: then dev.
	want, ok := os.LookupEnv("PROFINETCTL_E2E_VERSION")
	if !ok {
		t.Fatal("PROFINETCTL_E2E_VERSION is not set; run make e2e")
	}
	if want = strings.TrimPrefix(want, "v"); want == "" {
		want = "dev"
	}
	if !strings.HasPrefix(output, "profinetctl "+want+" (") || strings.Count(output, "\n") != 1 {
		t.Fatalf("output is not the line of profinetctl %s:\n%s", want, output)
	}
	output, code = run(t, "version", "--json")
	if code != 0 || !strings.HasPrefix(output, `{"name":"profinetctl","version":"`+want+`","tag":`) {
		t.Fatalf("version --json: exit %d\n%s", code, output)
	}
}

// An unknown command is a usage error: exit code 2 in docs/exitcodes.md.
func TestUnknownCommandIsAUsageError(t *testing.T) {
	output, code := run(t, "nosuchcommand")
	if code != 2 {
		t.Fatalf("exit code %d, want 2\n%s", code, output)
	}
	if !strings.Contains(output, `unknown command "nosuchcommand"`) {
		t.Fatalf("output does not name the unknown command:\n%s", output)
	}
}
