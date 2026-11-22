// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/otfabric/go-profinet/gsdml/typed"

	"github.com/otfabric/profinetctl/internal/errs"
)

// variant writes the GSDML file of the test model with old replaced by new.
func variant(t *testing.T, name, old, repl string) string {
	t.Helper()
	b, err := os.ReadFile(gsdmlFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(old)) {
		t.Fatalf("%q not in the test file", old)
	}
	return writeFile(t, name, bytes.Replace(b, []byte(old), []byte(repl), 1))
}

const schemaLoc = "GSDML-DeviceProfile-V2.35.xsd"

func TestSchemaVersions(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	var r versionsResult
	n.json(&r, "gsdml", "schema", "versions")
	if strings.Join(r.Versions, " ") != strings.Join(typed.Versions(), " ") || r.Versions[0] != "1.0" {
		t.Fatalf("%+v", r)
	}
	if out := n.ok("gsdml", "schema", "versions"); !strings.HasPrefix(out, "1.0\n2.0\n") {
		t.Fatalf("text:\n%s", out)
	}
	if out := n.ok("gsdml", "schema", "versions", "-o", "table"); !strings.Contains(out, "VERSION") || !strings.Contains(out, "2.46") {
		t.Fatalf("table:\n%s", out)
	}
}

func TestSchemaValidate(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	file := gsdmlFile(t)
	var r schemaValidateResult
	n.json(&r, "gsdml", "schema", "validate", file)
	if !r.Valid || r.SchemaVersion != "2.35" || len(r.Issues) != 0 || !strings.Contains(r.Scope, "not PI's GSDML checker") {
		t.Fatalf("%+v", r)
	}
	if out := n.ok("gsdml", "schema", "validate", file); !strings.Contains(out, "valid against GSDML V2.35") {
		t.Fatalf("text:\n%s", out)
	}
	// Against an older version, which does not have what the file has.
	out, _, code := n.run("gsdml", "schema", "validate", file, "--version", "1.0", "--format", "json")
	decode(t, out, &r)
	if code != 7 || r.Valid || r.SchemaVersion != "1.0" || len(r.Issues) == 0 || r.Issues[0].Line == 0 {
		t.Fatalf("1.0: exit %d %+v", code, r)
	}
	bad := variant(t, "bad.xml", "</DeviceIdentity>", "<FutureThing/></DeviceIdentity>")
	out, _, code = n.run("gsdml", "schema", "validate", bad)
	if code != 7 || !strings.Contains(out, "line ") || !strings.Contains(out, "FutureThing") || !strings.Contains(out, "1 issue(s) against GSDML V2.35") {
		t.Fatalf("bad: exit %d\n%s", code, out)
	}
	if out, _, _ := n.run("gsdml", "schema", "validate", bad, "-o", "table"); !strings.Contains(out, "LINE") || !strings.Contains(out, "FutureThing") {
		t.Fatalf("table:\n%s", out)
	}
	// More issues than a validation lists.
	var attrs strings.Builder
	for i := range typed.MaxIssues + 10 {
		attrs.WriteString(" a" + strings.Repeat("x", i%7) + strconv.Itoa(i) + `="1"`)
	}
	many := variant(t, "many.xml", "<DeviceIdentity ", "<DeviceIdentity"+attrs.String()+" ")
	out, _, code = n.run("gsdml", "schema", "validate", many, "--format", "json")
	decode(t, out, &r)
	if code != 7 || !r.Truncated || len(r.Issues) != typed.MaxIssues {
		t.Fatalf("many: exit %d, %d issues, truncated %v", code, len(r.Issues), r.Truncated)
	}
	if out, _, _ := n.run("gsdml", "schema", "validate", many); !strings.Contains(out, "more issues are not listed") {
		t.Fatal("many: no truncation note")
	}

	n.fails(2, "invalid_input", "gsdml", "schema", "validate", file, "--version", "9.9")
	n.fails(3, "input_file_open_failed", "gsdml", "schema", "validate", "/nonexistent.xml")
	n.fails(3, "invalid_gsdml", "gsdml", "schema", "validate", writeFile(t, "broken.xml", []byte("<ISO15745Profile>")))
	noVersion := variant(t, "noversion.xml", schemaLoc, "profile.xsd")
	n.fails(3, "unsupported_gsdml", "gsdml", "schema", "validate", noVersion)
	newer := variant(t, "newer.xml", schemaLoc, "GSDML-DeviceProfile-V2.99.xsd")
	if msg := n.fails(3, "unsupported_gsdml", "gsdml", "schema", "validate", newer); !strings.Contains(msg, "no schema for GSDML V2.99") {
		t.Fatalf("newer: %s", msg)
	}
	// --version applies when the file names none.
	n.json(&r, "gsdml", "schema", "validate", noVersion, "--version", "2.35")
	if !r.Valid {
		t.Fatalf("--version 2.35: %+v", r)
	}
}

func TestSchemaDump(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	file := gsdmlFile(t)
	var r struct {
		File          string `json:"file"`
		SchemaVersion string `json:"schema_version"`
		Validated     bool   `json:"validated"`
		Document      struct {
			ProfileHeader map[string]any
			ProfileBody   struct {
				DeviceIdentity struct{ VendorID, DeviceID string }
			}
		} `json:"document"`
	}
	n.json(&r, "gsdml", "schema", "dump", file)
	if r.SchemaVersion != "2.35" || !r.Validated || r.Document.ProfileBody.DeviceIdentity.VendorID == "" || len(r.Document.ProfileHeader) == 0 {
		t.Fatalf("%+v", r)
	}
	out := n.ok("gsdml", "schema", "dump", file)
	if !strings.HasPrefix(out, "{\n  \"ProfileHeader\": {") || strings.Contains(out, "null") {
		t.Fatalf("text:\n%.400s", out)
	}
	// Without validation: what the model holds and the schema refuses.
	dup := variant(t, "dup.xml", "<ProfileHeader>", "<ProfileHeader><ProfileIdentification>x</ProfileIdentification>")
	n.fails(3, "invalid_gsdml", "gsdml", "schema", "dump", dup)
	n.json(&r, "gsdml", "schema", "dump", dup, "--no-validate")
	if r.Validated {
		t.Fatalf("--no-validate: %+v", r)
	}
	// What the model cannot hold.
	bad := variant(t, "bad.xml", "</DeviceIdentity>", "<FutureThing/></DeviceIdentity>")
	n.fails(3, "invalid_gsdml", "gsdml", "schema", "dump", bad, "--no-validate")
	n.fails(3, "unsupported_gsdml", "gsdml", "schema", "dump", variant(t, "nov.xml", schemaLoc, "p.xsd"))
	n.fails(2, "invalid_input", "gsdml", "schema", "dump", file, "--version", "3")
	n.fails(3, "input_file_open_failed", "gsdml", "schema", "dump", "/nonexistent.xml")
}

// validate and check validate against the schema of the file's version, or
// with --against catalog against the catalog; a file naming no version
// with a schema is validated against the catalog.
func TestValidateAgainst(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	var r validateResult
	n.json(&r, "gsdml", "validate", gsdmlFile(t))
	if !r.Valid || r.SchemaVersion != "2.35" || !strings.Contains(r.Scope, "schema of version 2.35") {
		t.Fatalf("%+v", r)
	}
	bad := variant(t, "bad.xml", "</DeviceIdentity>", "<FutureThing/></DeviceIdentity>")
	out, _, code := n.run("gsdml", "validate", bad, "--format", "json")
	decode(t, out, &r)
	if code != 7 || r.Valid || len(r.Issues) != 1 || r.Issues[0].Line == 0 || r.Issues[0].Rule != "schema" {
		t.Fatalf("bad: exit %d %+v", code, r)
	}
	if out, _, _ := n.run("gsdml", "validate", bad, "-o", "table"); !strings.Contains(out, "LINE") {
		t.Fatalf("table:\n%s", out)
	}
	n.json(&r, "gsdml", "validate", bad, "--against", "catalog")
	if !r.Valid || !strings.Contains(r.Scope, "catalog") {
		t.Fatalf("against the catalog: %+v", r)
	}
	// inspect reports the validity validate reports: an element the schema
	// of 2.35 does not have makes the file invalid, though the catalog
	// accepts it.
	var in inspectResult
	n.json(&in, "gsdml", "inspect", bad)
	if in.Valid || in.Issues != 1 || !strings.Contains(in.Scope, "schema of version 2.35") {
		t.Fatalf("inspect bad: %+v", in)
	}
	if out := n.ok("gsdml", "inspect", bad); !strings.Contains(out, "Valid:    no, 1 issue(s)") || !strings.Contains(out, "Checked:  the GSDML schema of version 2.35") {
		t.Fatalf("inspect bad:\n%s", out)
	}
	n.json(&in, "gsdml", "inspect", bad, "--against", "catalog")
	if !in.Valid || in.Issues != 0 || !strings.Contains(in.Scope, "catalog") {
		t.Fatalf("inspect bad against the catalog: %+v", in)
	}
	n.fails(2, "invalid_input", "gsdml", "inspect", bad, "--against", "xsd")
	newer := variant(t, "newer.xml", schemaLoc, "GSDML-DeviceProfile-V2.99.xsd")
	n.json(&r, "gsdml", "validate", newer)
	if !r.Valid || !strings.Contains(r.Scope, "catalog") {
		t.Fatalf("newer: %+v", r)
	}
	var c checkJSON
	n.json(&c, "gsdml", "check", newer)
	if c.Unvalidated == 0 || !strings.Contains(c.Scope, "catalog") {
		t.Fatalf("check newer: %+v", c)
	}
	found := false
	for _, i := range c.Issues {
		found = found || (i.Rule == "schema" && i.Severity == "unvalidated" && strings.Contains(i.Message, "no schema for version 2.99"))
	}
	if !found {
		t.Fatalf("check newer: no unvalidated schema issue: %+v", c.Issues)
	}
	n.fails(2, "invalid_input", "gsdml", "validate", bad, "--against", "xsd")
	n.fails(3, "input_file_open_failed", "gsdml", "validate", "/nonexistent.xml")
	n.fails(3, "invalid_gsdml", "gsdml", "validate", writeFile(t, "broken.xml", []byte("<ISO15745Profile>")))
	n.fails(3, "invalid_gsdml", "gsdml", "check", writeFile(t, "broken2.xml", []byte("<ISO15745Profile>")))
}

func TestSchemaCommandsInterrupted(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	file := gsdmlFile(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, args := range [][]string{
		{"gsdml", "schema", "validate", file},
		{"gsdml", "schema", "dump", file},
		{"gsdml", "validate", "--against", "catalog", file},
	} {
		var out, errb bytes.Buffer
		if code := Run(ctx, n, append(args, "--format", "json"), &out, &errb); code != 130 {
			t.Fatalf("%s: exit %d, want 130\n%s%s", strings.Join(args, " "), code, out.String(), errb.String())
		}
	}
}

func TestPruneNulls(t *testing.T) {
	t.Parallel()
	got, err := pruneNulls([]byte(`{"b":null,"a":[1,{"x":null,"y":"s"},null,true],"c":{"d":null},"e":1.5}`))
	if err != nil || string(got) != `{"a":[1,{"y":"s"},null,true],"c":{},"e":1.5}` {
		t.Fatalf("%s %v", got, err)
	}
	for _, bad := range []string{``, `{`, `{"a":`, `{"a":{`, `[1,`, `{"a":[}`} {
		if _, err := pruneNulls([]byte(bad)); err == nil {
			t.Fatalf("%q: no error", bad)
		}
	}
}

func TestReadGSDMLBytesTooLarge(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	f := writeFile(t, "large.xml", make([]byte, 64<<20+1))
	n.fails(3, "", "gsdml", "schema", "validate", f)
}

func TestIssueLine(t *testing.T) {
	t.Parallel()
	if issueLine(0) != "" || issueLine(12) != "12" {
		t.Fatal(issueLine(0), issueLine(12))
	}
}

// A directory opens and does not read: input_file_open_failed.
func TestReadGSDMLBytesDirectory(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.fails(3, "input_file_open_failed", "gsdml", "schema", "validate", t.TempDir())
	n.fails(3, "input_file_open_failed", "gsdml", "validate", t.TempDir())
}

// With --version the file is not read for its version first: a file that
// is not XML fails in the validation.
func TestSchemaValidateVersionMalformed(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.fails(3, "invalid_gsdml", "gsdml", "schema", "validate", writeFile(t, "broken.xml", []byte("<ISO15745Profile>")), "--version", "2.35")
}

// xsd:float admits INF and NaN, which JSON does not.
func TestDocumentJSONUnrepresentable(t *testing.T) {
	t.Parallel()
	_, err := documentJSON("f.xml", struct{ PowerConsumption float64 }{math.Inf(1)})
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.CodeUnsupportedGSDML || !strings.Contains(e.Message, "f.xml") {
		t.Fatalf("%v", err)
	}
	raw, err := documentJSON("f.xml", struct {
		A *int
		B []any
	}{B: []any{nil, 1.5}})
	if err != nil || string(raw) != `{"B":[null,1.5]}` {
		t.Fatalf("%s %v", raw, err)
	}
	if _, err := (dumpResult{Document: json.RawMessage("{")}).MarshalTextOutput(); err == nil {
		t.Fatal("invalid document marshalled")
	}
}

func TestValidateAgainstCatalogMalformed(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	n.fails(3, "invalid_gsdml", "gsdml", "validate", "--against", "catalog", writeFile(t, "broken.xml", []byte("<ISO15745Profile>")))
}

// PowerConsumption is an xsd:float: INF is valid GSDML, and the dump says
// that JSON cannot hold it.
func TestSchemaDumpInfinity(t *testing.T) {
	t.Parallel()
	n := newTestNet(t)
	f := variant(t, "inf.xml", "</VirtualSubmoduleItem>", `<PROFIenergy ProfileVersion="V1.0"><EnergySavingModeList><EnergySavingModeItem ID="1" TimeToPause="1" RTTO="1" TimeMinLengthOfStay="1" PowerConsumption="INF"/></EnergySavingModeList></PROFIenergy></VirtualSubmoduleItem>`)
	if msg := n.fails(3, "unsupported_gsdml", "gsdml", "schema", "dump", f, "--no-validate"); !strings.Contains(msg, "INF or NaN") {
		t.Fatalf("%s", msg)
	}
}
