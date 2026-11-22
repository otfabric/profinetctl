// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/otfabric/go-profinet/gsdml"
	"github.com/otfabric/go-profinet/gsdml/typed"

	"github.com/otfabric/profinetctl/internal/errs"
)

// readGSDMLBytes reads a GSDML file, at most one octet more than
// go-profinet reads, which then refuses it as too large.
func readGSDMLBytes(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errs.InvalidInput(errs.CodeInputFileOpenFailed, fmt.Sprintf("GSDML file: %v", err), err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, gsdml.MaxDocumentSize+1))
	if err != nil {
		return nil, errs.InvalidInput(errs.CodeInputFileOpenFailed, fmt.Sprintf("GSDML file: %v", err), err)
	}
	return b, nil
}

// hasSchema reports whether go-profinet has the schema of a GSDML version.
func hasSchema(version string) bool { return slices.Contains(typed.Versions(), version) }

// schemaRange is the range of schema versions go-profinet has: "1.0 to 2.46".
func schemaRange() string {
	v := typed.Versions()
	return v[0] + " to " + v[len(v)-1]
}

// Against names what validate and check validate the structure of a file
// against.
const (
	againstSchema  = "schema"
	againstCatalog = "catalog"
)

// checkAgainst validates the --against flag.
func checkAgainst(s string) error {
	if s != againstSchema && s != againstCatalog {
		return errs.New(errs.KindUsage, errs.CodeInvalidInput, fmt.Sprintf("--against %q: want %s or %s", s, againstSchema, againstCatalog), nil)
	}
	return nil
}

// structureScope says what the structure of a file was validated against.
func structureScope(against, version string) string {
	if against == againstSchema && hasSchema(version) {
		return "the GSDML schema of version " + version + " (go-profinet's model generated from PI's reference schemas, checked against xmllint)"
	}
	return "go-profinet's version-independent GSDML catalog (structure, attribute types and references)"
}

// checkFile checks a GSDML file as gsdml check does: against the schema of
// its version (CheckBytes) or against the catalog (Check). The document
// is returned for the model conversions of check.
func checkFile(path, against string) (gsdml.Report, *gsdml.Document, error) {
	data, err := readGSDMLBytes(path)
	if err != nil {
		return gsdml.Report{}, nil, err
	}
	if against == againstCatalog {
		doc, err := gsdml.ParseBytes(data)
		if err != nil {
			return gsdml.Report{}, nil, gsdmlError(fmt.Sprintf("%s: %v", path, err), err)
		}
		return gsdml.Check(doc), doc, nil
	}
	rep, err := gsdml.CheckBytes(data)
	if err != nil {
		return gsdml.Report{}, nil, gsdmlError(fmt.Sprintf("%s: %v", path, err), err)
	}
	// CheckBytes read these octets with ParseBytes: they parse again.
	doc, _ := gsdml.ParseBytes(data)
	return rep, doc, nil
}

// issueText is an issue in text output: its line when it has one.
func issueText(i gsdml.Issue) string {
	if i.Line > 0 {
		return fmt.Sprintf("line %d: %s", i.Line, i)
	}
	return i.String()
}

// issueLine is the LINE column of a table: empty without a line.
func issueLine(line int) string {
	if line > 0 {
		return strconv.Itoa(line)
	}
	return ""
}

// versionsResult lists the GSDML schema versions go-profinet has.
type versionsResult struct {
	Versions []string `json:"versions"`
}

func (r versionsResult) MarshalTextOutput() (string, error) {
	return strings.Join(r.Versions, "\n") + "\n", nil
}

func (r versionsResult) TableHeaders() []string { return []string{"VERSION"} }

func (r versionsResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Versions))
	for _, v := range r.Versions {
		rows = append(rows, []string{v})
	}
	return rows
}

// schemaIssue is an issue of a schema validation.
type schemaIssue struct {
	Line    int    `json:"line,omitempty"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// schemaValidateResult is the outcome of gsdml schema validate.
type schemaValidateResult struct {
	File          string        `json:"file"`
	SchemaVersion string        `json:"schema_version"`
	Valid         bool          `json:"valid"`
	Issues        []schemaIssue `json:"issues"`
	// Truncated is set when the validation listed its first issues only.
	Truncated bool   `json:"truncated,omitempty"`
	Scope     string `json:"scope"`
}

func (r schemaValidateResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, i := range r.Issues {
		if i.Line > 0 {
			fmt.Fprintf(&b, "line %d: ", i.Line)
		}
		fmt.Fprintf(&b, "%s: %s\n", i.Path, i.Message)
	}
	if r.Truncated {
		fmt.Fprintf(&b, "more issues are not listed: the validation lists the first %d\n", typed.MaxIssues)
	}
	if r.Valid {
		fmt.Fprintf(&b, "%s: valid against GSDML V%s\n", r.File, r.SchemaVersion)
	} else {
		fmt.Fprintf(&b, "%s: %d issue(s) against GSDML V%s\n", r.File, len(r.Issues), r.SchemaVersion)
	}
	fmt.Fprintf(&b, "checked: %s\n", r.Scope)
	return b.String(), nil
}

func (r schemaValidateResult) TableHeaders() []string { return []string{"LINE", "PATH", "ISSUE"} }

func (r schemaValidateResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Issues))
	for _, i := range r.Issues {
		rows = append(rows, []string{issueLine(i.Line), i.Path, i.Message})
	}
	return rows
}

// Partial reports a file that is not valid: exit code 7.
func (r schemaValidateResult) Partial() bool { return !r.Valid }

const schemaValidateScope = "the GSDML schema of the version: structure, simple types with their facets, defaults and fixed values, and identity constraints (go-profinet's model generated from PI's reference schemas, checked against xmllint); not the semantic rules of gsdml check, and not PI's GSDML checker"

// schemaVersionOf is the version given, or the one the file names.
func schemaVersionOf(path string, data []byte, version string) (string, error) {
	if version != "" {
		if !hasSchema(version) {
			return "", errs.New(errs.KindUsage, errs.CodeInvalidInput, fmt.Sprintf("--version %q: go-profinet has the schemas %s", version, strings.Join(typed.Versions(), ", ")), nil)
		}
		return version, nil
	}
	v, err := typed.DetectVersion(data)
	if err != nil {
		return "", typedError(path, err)
	}
	if !hasSchema(v) {
		return "", errs.InvalidInput(errs.CodeUnsupportedGSDML, fmt.Sprintf("%s: go-profinet has no schema for GSDML V%s (it has %s); give --version", path, v, schemaRange()), typed.ErrUnknownVersion)
	}
	return v, nil
}

// typedError is the typed error of a file gsdml/typed refused: one naming
// no version it has is unsupported_gsdml, any other invalid_gsdml.
func typedError(path string, err error) error {
	msg := fmt.Sprintf("%s: %v", path, err)
	if errors.Is(err, typed.ErrUnknownVersion) {
		return errs.InvalidInput(errs.CodeUnsupportedGSDML, msg+"; give --version", err)
	}
	return gsdmlError(msg, err)
}

func schemaValidate(ctx context.Context, path, version string) (schemaValidateResult, error) {
	data, err := readGSDMLBytes(path)
	if err != nil {
		return schemaValidateResult{}, err
	}
	v, err := schemaVersionOf(path, data, version)
	if err != nil {
		return schemaValidateResult{}, err
	}
	r := schemaValidateResult{File: path, SchemaVersion: v, Valid: true, Issues: []schemaIssue{}, Scope: schemaValidateScope}
	err = typed.ValidateVersion(data, v)
	if se, ok := errors.AsType[*typed.SchemaError](err); ok {
		r.Valid, r.Truncated = false, se.Truncated
		for _, i := range se.Issues {
			r.Issues = append(r.Issues, schemaIssue{Line: i.Line, Path: i.Path, Message: i.Message})
		}
	} else if err != nil {
		return schemaValidateResult{}, typedError(path, err)
	}
	return r, interrupted(ctx)
}

// dumpResult is the typed model of a document.
type dumpResult struct {
	File          string `json:"file"`
	SchemaVersion string `json:"schema_version"`
	// Validated is false with --no-validate: the document was decoded
	// without being validated against its schema.
	Validated bool `json:"validated"`
	// Document is the root element, ISO15745Profile, with the elements
	// and attributes the file has (absent ones are left out).
	Document json.RawMessage `json:"document"`
}

func (r dumpResult) MarshalTextOutput() (string, error) {
	b, err := json.MarshalIndent(r.Document, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// pruneNulls removes the null members of the objects of a JSON value (the
// optional elements and attributes a document does not have), keeping the
// order of the others: that of the schema.
func pruneNulls(raw []byte) ([]byte, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var out bytes.Buffer
	if _, err := pruneValue(d, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// pruneValue copies the next value of d to out without null members, and
// reports whether the value is null (then nothing is written).
func pruneValue(d *json.Decoder, out *bytes.Buffer) (null bool, err error) {
	t, err := d.Token()
	if err != nil {
		return false, err
	}
	switch t {
	case nil:
		return true, nil
	case json.Delim('{'):
		out.WriteByte('{')
		first := true
		var member bytes.Buffer
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return false, err
			}
			member.Reset()
			null, err := pruneValue(d, &member)
			if err != nil {
				return false, err
			}
			if null {
				continue
			}
			if !first {
				out.WriteByte(',')
			}
			first = false
			kb, _ := json.Marshal(k)
			out.Write(kb)
			out.WriteByte(':')
			out.Write(member.Bytes())
		}
		_, err = d.Token()
		out.WriteByte('}')
		return false, err
	case json.Delim('['):
		out.WriteByte('[')
		for i := 0; d.More(); i++ {
			if i > 0 {
				out.WriteByte(',')
			}
			null, err := pruneValue(d, out)
			if err != nil {
				return false, err
			}
			if null {
				out.WriteString("null")
			}
		}
		_, err = d.Token()
		out.WriteByte(']')
		return false, err
	}
	b, _ := json.Marshal(t) // a string, json.Number or bool from the decoder
	out.Write(b)
	return false, nil
}

// documentJSON is the JSON of a typed document without its absent
// members. A float value JSON cannot represent (INF, -INF or NaN, valid
// in xsd:float) is unsupported_gsdml.
func documentJSON(path string, root any) (json.RawMessage, error) {
	raw, err := json.Marshal(root)
	if err == nil {
		raw, err = pruneNulls(raw)
	}
	if err != nil {
		return nil, errs.InvalidInput(errs.CodeUnsupportedGSDML, fmt.Sprintf("%s: the typed model has a value JSON cannot represent (a floating-point INF or NaN): %v", path, err), err)
	}
	return raw, nil
}

func schemaDump(ctx context.Context, path, version string, validate bool) (dumpResult, error) {
	data, err := readGSDMLBytes(path)
	if err != nil {
		return dumpResult{}, err
	}
	v, err := schemaVersionOf(path, data, version)
	if err != nil {
		return dumpResult{}, err
	}
	var d typed.Versioned
	if validate {
		d, err = typed.ParseVersion(data, v)
	} else {
		d, err = typed.DecodeVersion(data, v)
	}
	if err != nil {
		return dumpResult{}, typedError(path, err)
	}
	if err := interrupted(ctx); err != nil {
		return dumpResult{}, err
	}
	raw, err := documentJSON(path, d.RootElement())
	if err != nil {
		return dumpResult{}, err
	}
	return dumpResult{File: path, SchemaVersion: v, Validated: validate, Document: raw}, nil
}

func newGSDMLSchemaCmd(a *app) *cobra.Command {
	g := groupCmd("schema", "The GSDML schema versions: list them, validate a file against one, show a file's typed model")
	versionsCmd := &cobra.Command{
		Use:   "versions",
		Short: "List the GSDML schema versions go-profinet has",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return a.result(c, func(context.Context) (any, error) { return versionsResult{Versions: typed.Versions()}, nil })
		},
	}
	var valVersion string
	validateCmd := &cobra.Command{
		Use:   "validate FILE",
		Short: "Validate a GSDML file against the schema of its version only; exit code 7 when it is not valid",
		Long: `Validate a GSDML file against the GSDML schema of the version it names in
xsi:schemaLocation, or of --version: the structure, the simple types with
their facets, defaults and fixed values, and the identity constraints, each
issue with its line. go-profinet's validator is generated from PI's reference
schemas and checked against xmllint; the schemas themselves are not part of
profinetctl. gsdml validate and gsdml check add go-profinet's semantic rules.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return a.result(c, func(ctx context.Context) (any, error) { return schemaValidate(ctx, args[0], valVersion) })
		},
	}
	validateCmd.Flags().StringVar(&valVersion, "version", "", "Schema version to validate against, such as 2.35 (default: the one the file names)")
	var dumpVersion string
	var noValidate bool
	dumpCmd := &cobra.Command{
		Use:   "dump FILE",
		Short: "Show the typed model of a GSDML file as JSON",
		Long: `Decode a GSDML file with the model of its schema version (or --version) and
print it as JSON: every element and attribute the file has, with the names
of the schema, values of numeric and boolean types as JSON numbers and
booleans. The file is validated against the schema first; --no-validate
decodes what the model can hold without validating it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return a.result(c, func(ctx context.Context) (any, error) { return schemaDump(ctx, args[0], dumpVersion, !noValidate) })
		},
	}
	dumpCmd.Flags().StringVar(&dumpVersion, "version", "", "Schema version to decode as, such as 2.35 (default: the one the file names)")
	dumpCmd.Flags().BoolVar(&noValidate, "no-validate", false, "Decode without validating against the schema")
	g.AddCommand(versionsCmd, validateCmd, dumpCmd)
	return g
}
