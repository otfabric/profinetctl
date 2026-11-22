// SPDX-License-Identifier: MIT

package cmd

import (
	"os"
	"slices"
	"strings"
	"testing"
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
	// --param of another record goes with the plan.
	n.ok(append(append([]string{}, base...), "--gsdml", gf, "--param-defaults", "--param", "2:1:17:00")...)
}
