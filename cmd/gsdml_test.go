// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/otfabric/go-profinet/gsdml"
)

// dapsFile writes the GSDML file of the test model with its device access
// point repeated n times, under distinct IDs.
func dapsFile(t *testing.T, n int) string {
	t.Helper()
	doc, err := gsdml.FromModel(testModel(), gsdml.GenerateOptions{SupervisorAR: true})
	if err != nil {
		t.Fatal(err)
	}
	list := doc.ProfileBody.ApplicationProcess.DeviceAccessPointList
	first := list.DeviceAccessPointItems[0]
	for i := 1; i < n; i++ {
		c := *first
		c.ID = first.ID + "_" + strconv.Itoa(i)
		list.DeviceAccessPointItems = append(list.DeviceAccessPointItems, &c)
	}
	b, err := gsdml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return writeFile(t, "daps.xml", b)
}

// gsdml inspect converts the first 64 device access points; it lists the
// others with their ID and ident number, not converted.
func TestInspectConvertsTheFirstDAPs(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	for _, count := range []int{1, inspectDAPs - 1, inspectDAPs, inspectDAPs + 1, 200} {
		var r inspectResult
		n.json(&r, "gsdml", "inspect", dapsFile(t, count))
		if len(r.DAPs) != count {
			t.Fatalf("%d DAPs: %d listed", count, len(r.DAPs))
		}
		for i, d := range r.DAPs {
			converted := d.Name != "" && d.Error == ""
			if converted != (i < inspectDAPs) {
				t.Fatalf("%d DAPs: DAP %d (%s) converted %v: %+v", count, i, d.ID, converted, d)
			}
			if i >= inspectDAPs && (d.Error != "not converted: more than 64 device access points" || d.IdentNumber == "" || d.ID == "") {
				t.Fatalf("%d DAPs: DAP %d %+v", count, i, d)
			}
		}
	}
}

// uniqueDAPsFile is dapsFile with the submodule IDs of each copy made
// unique: check converts to a model only a file without errors.
func uniqueDAPsFile(t *testing.T, n int) string {
	t.Helper()
	b, err := os.ReadFile(dapsFile(t, n))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	b = regexp.MustCompile(`ID="(DAP_SM1|IF32768|Port32769)"`).ReplaceAllFunc(b, func(m []byte) []byte {
		id := string(m[4 : len(m)-1])
		seen[id]++
		return fmt.Appendf(nil, `ID="%s_%d"`, id, seen[id])
	})
	return writeFile(t, "unique-daps.xml", b)
}

// gsdml check converts the first 64 device access points to a model; the
// others are one unvalidated issue, not skipped in silence.
func TestCheckReportsUnconvertedDAPs(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	for _, count := range []int{inspectDAPs, inspectDAPs + 1, 200} {
		var c checkJSON
		out, _, code := n.run("gsdml", "check", uniqueDAPsFile(t, count), "--format", "json")
		decode(t, out, &c)
		var unconverted []string
		for _, i := range c.Issues {
			if i.Rule == ruleModel {
				if i.Severity != "unvalidated" || !strings.HasSuffix(i.Path, "/DeviceAccessPointList") {
					t.Fatalf("%d DAPs: %+v", count, i)
				}
				unconverted = append(unconverted, i.Message)
			}
		}
		if count <= inspectDAPs {
			if len(unconverted) != 0 || code != 0 {
				t.Fatalf("%d DAPs: exit %d %v", count, code, unconverted)
			}
			continue
		}
		want := fmt.Sprintf("%d of %d device access points not converted to a model: check converts the first 64", count-inspectDAPs, count)
		if len(unconverted) != 1 || unconverted[0] != want || c.Unvalidated == 0 || code != 0 {
			t.Fatalf("%d DAPs: exit %d %+v", count, code, c)
		}
		if _, _, code := n.run("gsdml", "check", uniqueDAPsFile(t, count), "--strict"); code != 7 {
			t.Fatalf("%d DAPs --strict: exit %d", count, code)
		}
	}
}

// The gsdml commands end with exit code 130 when their context has ended
// (Ctrl-C, SIGTERM), instead of writing a result.
func TestGSDMLCommandsInterrupted(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	file, model := gsdmlFile(t), modelFile(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, args := range [][]string{
		{"gsdml", "inspect", dapsFile(t, 3)},
		{"gsdml", "validate", file},
		{"gsdml", "check", file},
		{"gsdml", "modules", file},
		{"gsdml", "submodules", file},
		{"gsdml", "gsdx", gsdxFile(t, n, false)},
		{"gsdml", "generate", "--model", model},
	} {
		var out, errb bytes.Buffer
		code := Run(ctx, n, append(args, "--format", "json"), &out, &errb)
		if code != 130 || !strings.Contains(out.String(), `"kind": "interrupted"`) {
			t.Fatalf("%s: exit %d, want 130\nstdout: %s\nstderr: %s", strings.Join(args[:2], " "), code, out.String(), errb.String())
		}
	}
	if err := interrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := interrupted(ctx); err == nil || !strings.Contains(fmt.Sprint(err), "context canceled") {
		t.Fatalf("interrupted: %v", err)
	}
}
