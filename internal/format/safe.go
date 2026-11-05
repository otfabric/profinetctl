// SPDX-License-Identifier: MIT

package format

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Much of what profinetctl prints comes from devices: station names,
// type names, LLDP strings, I&M text. A device can put terminal control
// sequences in them (an ESC that retitles the window or clears the
// screen, a carriage return that overwrites a line). Text and table
// output therefore escape control characters and invalid UTF-8 before
// they reach the terminal; JSON is escaped by encoding/json.

// SafeText escapes every C0 control except newline and tab, DEL, every C1
// control and invalid UTF-8 as \xNN or \u00NN, so text output keeps its
// own line structure but cannot carry a terminal escape sequence.
func SafeText(s string) string { return escape(s, true) }

// SafeCell is SafeText for a single table cell or line: newline and tab
// are escaped too, so a value cannot add a row or shift a column.
func SafeCell(s string) string { return escape(s, false) }

func escape(s string, keepLayout bool) string {
	if !needsEscape(s, keepLayout) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && n == 1:
			_, _ = fmt.Fprintf(&b, `\x%02x`, s[i])
		case keepLayout && (r == '\n' || r == '\t'):
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case isControl(r):
			_, _ = fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

func needsEscape(s string, keepLayout bool) bool {
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && n == 1 {
			return true
		}
		if isControl(r) && (!keepLayout || (r != '\n' && r != '\t')) {
			return true
		}
		i += n
	}
	return false
}

// isControl reports C0, DEL and C1 controls.
func isControl(r rune) bool { return r < 0x20 || (r >= 0x7F && r < 0xA0) }
