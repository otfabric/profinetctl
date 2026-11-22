// SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/otfabric/go-profinet"
	"github.com/otfabric/go-profinet/gsdml"
	"github.com/otfabric/profinetctl/internal/errs"
)

// paramRecord replaces record 16 of slot 2's submodule, which the
// generated file describes by a Const, by two fields: MODE (Unsigned8,
// AllowedValues 0..3, default 1) and a field "Outputs" that is not
// Changeable (default 5).
const paramRecord = `<ParameterRecordDataItem Index="16" Length="2">
                  <Name TextId="M2_SM1_R16"></Name>
                  <Ref DataType="Unsigned8" ByteOffset="0" DefaultValue="1" AllowedValues="0..3" ID="MODE" TextId="M2_SM1_R16"></Ref>
                  <Ref DataType="Unsigned8" ByteOffset="1" DefaultValue="5" Changeable="false" TextId="M2_SM1_Name"></Ref>
                </ParameterRecordDataItem>`

// paramGSDMLFile is gsdmlFile with record 16 replaced by rec.
func paramGSDMLFile(t *testing.T, rec string) string {
	t.Helper()
	b, err := os.ReadFile(gsdmlFile(t))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, `<ParameterRecordDataItem Index="16"`)
	j := strings.Index(s, `</ParameterRecordDataItem>`)
	if i < 0 || j < i {
		t.Fatal("no record 16 in the generated file")
	}
	return writeFile(t, "params.xml", []byte(s[:i]+rec+s[j+len(`</ParameterRecordDataItem>`):]))
}

func TestGSDMLPlan(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	gf := paramGSDMLFile(t, paramRecord)
	var r planResult
	// By ID and by name: the record with the value set, the other field
	// at its default.
	for _, key := range []string{"MODE", "Mode"} {
		n.json(&r, "gsdml", "plan", gf, "--param-value", "2:1:16:"+key+"=3")
		if !r.Planned || len(r.Writes) != 1 || r.Writes[0].Data != "0305" || r.Writes[0].Values != 1 ||
			r.Writes[0].Slot != 2 || r.Writes[0].Subslot != 1 || r.Writes[0].Index != 16 || r.Writes[0].SubmoduleIdent != 0x21 {
			t.Fatalf("%s: %+v", key, r)
		}
	}
	// Defaults: every record of the plugged submodules.
	n.json(&r, "gsdml", "plan", gf, "--param-defaults")
	if !r.Planned || len(r.Writes) != 1 || r.Writes[0].Data != "0105" || r.Writes[0].Values != 0 {
		t.Fatalf("defaults: %+v", r)
	}
	// Nothing asked: an empty plan.
	n.json(&r, "gsdml", "plan", gf)
	if !r.Planned || len(r.Writes) != 0 || r.Issues == nil {
		t.Fatalf("empty: %+v", r)
	}
	// Text and table.
	if out := n.ok("gsdml", "plan", gf, "--param-value", "2:1:16:MODE=2"); !strings.Contains(out, "slot 2 subslot 0x0001 record 16") || !strings.Contains(out, ": 0205") ||
		!strings.Contains(out, "1 record write(s)") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("gsdml", "plan", gf, "--param-defaults", "--format", "table"); !strings.Contains(out, "SLOT") || !strings.Contains(out, "0105") {
		t.Fatalf("table:\n%s", out)
	}
	// A module plugged with --plug is planned too, in its slot.
	n.json(&r, "gsdml", "plan", gf, "--plug", "1=0x10", "--param-defaults")
	if !r.Planned || len(r.Writes) != 1 {
		t.Fatalf("plug: %+v", r)
	}
}

func TestGSDMLPlanErrors(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	gf := paramGSDMLFile(t, paramRecord)
	for _, bad := range []string{
		"2:1:16",           // no field
		"2:1:16:=3",        // empty field
		"2:1:16:MODE",      // no value
		"x:1:16:MODE=1",    // slot
		"2:1:16:MODE=9",    // outside AllowedValues
		"2:1:16:Outputs=1", // not Changeable
		"2:1:16:NOPE=1",    // no such field
		"2:1:17:MODE=1",    // no such record
		"3:1:16:MODE=1",    // no such record in slot 3
	} {
		n.fails(3, "invalid_input", "gsdml", "plan", gf, "--param-value", bad)
	}
	// A field twice: refused, not overwritten.
	if msg := n.fails(3, "invalid_input", "gsdml", "plan", gf, "--param-value", "2:1:16:MODE=1", "--param-value", "2:1:16:MODE=2"); !strings.Contains(msg, "given twice") {
		t.Fatalf("twice: %s", msg)
	}
	// Errors of the record itself: the plan is refused with them, exit 7.
	overlap := strings.Replace(paramRecord, `<Ref DataType="Unsigned8" ByteOffset="0"`,
		`<Const ByteOffset="0" Data="0x00"></Const><Const ByteOffset="0" Data="0x01"></Const><Ref DataType="Unsigned8" ByteOffset="0"`, 1)
	out, errOut, code := n.run("gsdml", "plan", paramGSDMLFile(t, overlap), "--param-defaults", "--format", "json")
	var r planResult
	decode(t, out, &r)
	if code != 7 || r.Planned || len(r.Writes) != 0 || r.Errors == 0 {
		t.Fatalf("refused: %d %+v\n%s", code, r, errOut)
	}
	if out, _, code := n.run("gsdml", "plan", paramGSDMLFile(t, overlap), "--param-defaults"); code != 7 || !strings.Contains(out, "refused:") {
		t.Fatalf("refused text: %d\n%s", code, out)
	}
	// The file and the flags.
	n.fails(3, "input_file_open_failed", "gsdml", "plan", gf+".none")
	n.fails(3, "invalid_input", "gsdml", "plan", gf, "--plug", "x")
	n.fails(2, "invalid_input", "gsdml", "plan")
	many := []string{"gsdml", "plan", gf}
	for range maxParamValues + 1 {
		many = append(many, "--param-value", "2:1:16:MODE=1")
	}
	if msg := n.fails(3, "invalid_input", many...); !strings.Contains(msg, "more than") {
		t.Fatalf("many: %s", msg)
	}
}

// An AR writes the planned records before PrmEnd: the device stores them.
func TestConnectParamValue(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	d := n.device("dev-a", "192.168.0.10")
	gf := paramGSDMLFile(t, paramRecord)
	n.ok("-i", "pn0", "connect", "-s", "dev-a", "--gsdml", gf, "--param-value", "2:1:16:MODE=2", "--duration", "300ms", "--format", "json")
	if w := d.h.recordWrites(); !slices.Contains(w, "2:1:16=0205") {
		t.Fatalf("writes %v", w)
	}
}

func TestConnectParamValueErrors(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.device("dev-a", "192.168.0.10")
	gf := paramGSDMLFile(t, paramRecord)
	base := []string{"-i", "pn0", "connect", "-s", "dev-a", "--duration", "100ms"}
	// A model in JSON has no fields.
	n.fails(2, "invalid_flag_combination", append(append([]string{}, base...), "--model", modelFile(t), "--param-defaults")...)
	// --param and a planned write of one record.
	n.fails(2, "invalid_flag_combination", append(append([]string{}, base...), "--gsdml", gf, "--param-value", "2:1:16:MODE=1", "--param", "2:1:16:0000")...)
	// A value the file does not allow: nothing is sent.
	n.fails(3, "invalid_input", append(append([]string{}, base...), "--gsdml", gf, "--param-value", "2:1:16:MODE=9")...)
	n.fails(3, "invalid_input", append(append([]string{}, base...), "--gsdml", gf, "--param-value", "bad")...)
	// --param of another record goes with the plan, after it, with a
	// warning that it is not checked.
	_, errOut, code := n.run(append(append([]string{}, base...), "--gsdml", gf, "--param-defaults", "--param", "2:1:17:00")...)
	if code != 0 || !strings.Contains(errOut, "warning: --param records are written after the parameter plan's") {
		t.Fatalf("--param with a plan: %d\n%s", code, errOut)
	}
	// The warnings of the plan go to stderr, with their severity.
	_, errOut, code = n.run(append(append([]string{}, base...), "--gsdml", paramGSDMLFile(t, warnRecord), "--param-defaults")...)
	if code != 0 || !strings.Contains(errOut, "warning: parameter plan: warning: ") {
		t.Fatalf("plan warnings: %d\n%s", code, errOut)
	}
}

// warnRecord is paramRecord with a default outside MODE's AllowedValues: a
// warning of the plan, not an error.
var warnRecord = strings.Replace(paramRecord, `DefaultValue="1" AllowedValues="0..3"`, `DefaultValue="7" AllowedValues="0..3"`, 1)

// Warnings do not stop a plan; every output shows them, a table on stderr.
func TestGSDMLPlanWarnings(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	gf := paramGSDMLFile(t, warnRecord)
	var r planResult
	n.json(&r, "gsdml", "plan", gf, "--param-defaults")
	if !r.Planned || len(r.Writes) != 1 || r.Writes[0].Data != "0705" || r.Warnings == 0 || len(r.Issues) != r.Warnings+r.Unvalidated {
		t.Fatalf("%+v", r)
	}
	if out := n.ok("gsdml", "plan", gf, "--param-defaults"); !strings.Contains(out, "warning: ") || !strings.Contains(out, "1 warning(s)") {
		t.Fatalf("text:\n%s", out)
	}
	out, errOut, code := n.run("gsdml", "plan", gf, "--param-defaults", "--format", "table")
	if code != 0 || !strings.Contains(out, "0705") || strings.Contains(out, "warning") || !strings.Contains(errOut, "warning: ") {
		t.Fatalf("table %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	// A table of a refused plan: no rows, the errors on stderr, exit 7.
	overlap := strings.Replace(paramRecord, `<Ref DataType="Unsigned8" ByteOffset="0"`,
		`<Const ByteOffset="0" Data="0x00"></Const><Const ByteOffset="0" Data="0x01"></Const><Ref DataType="Unsigned8" ByteOffset="0"`, 1)
	if _, errOut, code := n.run("gsdml", "plan", paramGSDMLFile(t, overlap), "--param-defaults", "--format", "table"); code != 7 || !strings.Contains(errOut, "error: ") {
		t.Fatalf("refused table %d\n%s", code, errOut)
	}
}

// --slot plans the modules an AR with the same --slot owns.
func TestGSDMLPlanSlot(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	gf := paramGSDMLFile(t, paramRecord)
	var r planResult
	n.json(&r, "gsdml", "plan", gf, "--param-defaults", "--slot", "2")
	if len(r.Writes) != 1 || r.Writes[0].Slot != 2 {
		t.Fatalf("slot 2: %+v", r)
	}
	n.json(&r, "gsdml", "plan", gf, "--param-defaults", "--slot", "1,3")
	if !r.Planned || len(r.Writes) != 0 {
		t.Fatalf("slots 1 and 3: %+v", r)
	}
	// A value for a record of a slot the AR does not own.
	n.fails(3, "invalid_input", "gsdml", "plan", gf, "--slot", "1", "--param-value", "2:1:16:MODE=1")
	n.fails(3, "invalid_input", "gsdml", "plan", gf, "--slot", "9")
	n.fails(3, "invalid_input", "gsdml", "plan", gf, "--slot", "0")
}

func TestPlanHelpers(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 100)
	if c := clip(long); len(c) >= len(long) || !strings.HasSuffix(c, "…") || clip("ab") != "ab" {
		t.Fatalf("clip %q", c)
	}
	for _, c := range []struct {
		err  error
		code string
	}{
		{fmt.Errorf("x: %w", profinet.ErrUnsupported), "unsupported_gsdml"},
		{fmt.Errorf("x: %w", profinet.ErrMalformed), "invalid_gsdml"},
		{fmt.Errorf("x: %w", profinet.ErrInvalidConfig), "invalid_input"},
	} {
		if e, ok := errs.As(planError(c.err)); !ok || e.Code != c.code || e.Kind != errs.KindInvalidInput {
			t.Fatalf("%v: %+v", c.err, e)
		}
	}
	if w := planWarnings(gsdml.ParameterPlan{Issues: []gsdml.Issue{{Severity: gsdml.SeverityError}, {Severity: gsdml.SeverityWarning}, {Severity: gsdml.SeverityUnvalidated}}}); len(w) != 2 {
		t.Fatalf("warnings %v", w)
	}
	r := newPlanResult("f", "", gsdml.ParameterPlan{Issues: []gsdml.Issue{{Severity: gsdml.SeverityUnvalidated, Path: "p", Message: "m"}},
		Writes: []gsdml.PlannedWrite{{Write: gsdml.RecordWrite{Slot: 1, Subslot: 1, Index: 2, Data: []byte{1}}}}}, true)
	if out, _ := r.MarshalTextOutput(); r.Unvalidated != 1 || !strings.Contains(out, "record 2 -") || !strings.Contains(out, "unvalidated: p: m") {
		t.Fatalf("%+v\n%s", r, out)
	}
	if msg := (func() string {
		_, err := parseParamValues([]string{strings.Repeat("y", 100)})
		e, _ := errs.As(err)
		return e.Message
	})(); !strings.Contains(msg, "…") {
		t.Fatalf("clipped message %q", msg)
	}
}
