// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/otfabric/go-profinet"
	"github.com/otfabric/go-profinet/dcp"
)

func TestIdentifyTruncated(t *testing.T) {
	ids := []dcp.Identity{{NameOfStation: "a"}}
	exhausted := fmt.Errorf("dcp: identify: %w: more than 1 stations answered", profinet.ErrResourceExhausted)
	for _, c := range []struct {
		name string
		ids  []dcp.Identity
		err  error
		want bool
	}{
		{"no error", ids, nil, false},
		{"more answers than kept", ids, exhausted, true},
		{"resource exhausted, nothing kept", nil, exhausted, false},
		{"timeout", ids, profinet.ErrTimeout, false},
		{"closed", ids, profinet.ErrClosed, false},
	} {
		if got := identifyTruncated(c.ids, c.err); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// The truncated marker alone makes a result partial (exit code 7), and it
// shows only when set.
func TestTruncatedResults(t *testing.T) {
	if !(discoverResult{Truncated: true}).Partial() || (discoverResult{}).Partial() {
		t.Fatal("discoverResult.Partial")
	}
	if !(topologyResult{Truncated: true}).Partial() || (topologyResult{}).Partial() {
		t.Fatal("topologyResult.Partial")
	}
	if s, _ := (discoverResult{Truncated: true, kept: 7}).MarshalTextOutput(); !strings.HasSuffix(s, "0 device(s)\n"+truncatedLine(7)) {
		t.Fatalf("discover text: %q", s)
	}
	if s, _ := (topologyResult{Truncated: true, kept: 7}).MarshalTextOutput(); s != truncatedLine(7) {
		t.Fatalf("topology text: %q", s)
	}
	for _, v := range []interface {
		MarshalTextOutput() (string, error)
	}{discoverResult{}, topologyResult{}} {
		if s, _ := v.MarshalTextOutput(); strings.Contains(s, "truncated") {
			t.Fatalf("%T text: %q", v, s)
		}
		if b, _ := json.Marshal(v); strings.Contains(string(b), "truncated") {
			t.Fatalf("%T JSON: %s", v, b)
		}
	}
}

func TestIMWriteResultOutput(t *testing.T) {
	change := changeResult{MAC: "02:00:00:00:00:01", Station: "dev", Operation: "im write", Value: "I&M1"}
	partial := imWriteResult{changeResult: change, Written: []string{"I&M1"},
		Failed: &imWriteFailure{Record: "I&M2", Kind: "refused", Code: "pnio_error", Message: "write I&M2: refused"}}
	full := imWriteResult{changeResult: change, Written: []string{"I&M1"}}
	if !partial.Partial() || full.Partial() {
		t.Fatal("Partial")
	}
	if s, _ := partial.MarshalTextOutput(); s != "im write: 02:00:00:00:00:01 dev → I&M1\nfailed: write I&M2: refused\n" {
		t.Fatalf("text: %q", s)
	}
	if s, _ := full.MarshalTextOutput(); s != "im write: 02:00:00:00:00:01 dev → I&M1\n" {
		t.Fatalf("text: %q", s)
	}
	if rows := partial.TableRows(); len(rows) != 1 || rows[0][3] != "I&M1; failed: write I&M2: refused" {
		t.Fatalf("table: %q", rows)
	}
	if rows := full.TableRows(); len(rows) != 1 || rows[0][3] != "I&M1" || len(partial.TableHeaders()) != len(rows[0]) {
		t.Fatalf("table: %q", rows)
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(partial); err != nil || b.String() != `{"mac":"02:00:00:00:00:01","station":"dev","operation":"im write","value":"I&M1","written":["I&M1"],`+
		`"failed":{"record":"I&M2","kind":"refused","code":"pnio_error","message":"write I&M2: refused"}}`+"\n" {
		t.Fatalf("JSON: %s %v", b.String(), err)
	}
	if b, err := json.Marshal(full); err != nil || strings.Contains(string(b), "failed") {
		t.Fatalf("JSON: %s %v", b, err)
	}
}
