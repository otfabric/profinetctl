// SPDX-License-Identifier: MIT

package format

import (
	"bytes"
	"strings"
	"testing"
)

func TestSafe(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, text, cell string }{
		{"plain station-1", "plain station-1", "plain station-1"},
		{"Grüße 日本", "Grüße 日本", "Grüße 日本"},
		{"a\nb\tc", "a\nb\tc", `a\nb\tc`},
		{"\x1b]0;pwned\x07\x1b[2J\rfake", `\u001b]0;pwned\u0007\u001b[2J\rfake`, `\u001b]0;pwned\u0007\u001b[2J\rfake`},
		{"del\x7f c1\u009b", `del\u007f c1\u009b`, `del\u007f c1\u009b`},
		{"bad\xff\xfeutf8", `bad\xff\xfeutf8`, `bad\xff\xfeutf8`},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := SafeText(c.in); got != c.text {
			t.Errorf("SafeText(%q) = %q, want %q", c.in, got, c.text)
		}
		if got := SafeCell(c.in); got != c.cell {
			t.Errorf("SafeCell(%q) = %q, want %q", c.in, got, c.cell)
		}
	}
}

type hostile struct{}

const evil = "dev\x1b[2J\n\tforged"

func (hostile) MarshalTextOutput() (string, error) { return "name " + evil + "\n", nil }
func (hostile) TableHeaders() []string             { return []string{"NAME", "H\x1b"} }
func (hostile) TableRows() [][]string              { return [][]string{{evil, "x"}} }

// TestOutputEscapesDeviceStrings: neither text nor table output lets a control
// sequence through, and a table value cannot add a row.
func TestOutputEscapesDeviceStrings(t *testing.T) {
	t.Parallel()
	check := func(name, out string, lines int) {
		t.Helper()
		if strings.ContainsAny(out, "\x1b\r") {
			t.Errorf("%s: control character in %q", name, out)
		}
		if n := strings.Count(out, "\n"); n != lines {
			t.Errorf("%s: %d lines in %q, want %d", name, n, out, lines)
		}
	}
	for _, f := range []OutputFormat{FormatText, FormatTable} {
		var b bytes.Buffer
		if err := Write(&b, f, hostile{}); err != nil {
			t.Fatal(err)
		}
		switch f {
		case FormatText:
			// Text keeps the newline of the value; the escape is gone.
			check("text", b.String(), 2)
		case FormatTable:
			check("table", b.String(), 3)
		case FormatJSON:
		}
	}
	var b bytes.Buffer
	s := NewStreamWriter(&b, FormatTable)
	for range 2 {
		if err := s.Write(hostile{}); err != nil {
			t.Fatal(err)
		}
	}
	check("stream table", b.String(), 4)
	b.Reset()
	if err := NewStreamWriter(&b, FormatText).Write(hostile{}); err != nil {
		t.Fatal(err)
	}
	check("stream text", b.String(), 2)
}
