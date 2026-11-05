// SPDX-License-Identifier: MIT

package format

import (
	"fmt"
	"strings"
	"text/tabwriter"
)

// safeRow returns the cells escaped with SafeCell.
func safeRow(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = SafeCell(c)
	}
	return out
}

// RenderTable renders headers and rows as column-aligned text using tabwriter.
func RenderTable(m TableMarshaler) (string, error) {
	headers := safeRow(m.TableHeaders())
	rows := m.TableRows()
	if len(headers) == 0 {
		return "", fmt.Errorf("table has no headers")
	}
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.Join(headers, "\t"))
	rule := make([]string, len(headers))
	for i := range rule {
		rule[i] = "-"
	}
	_, _ = fmt.Fprintln(tw, strings.Join(rule, "\t"))
	for _, row := range rows {
		if len(row) != len(headers) {
			return "", fmt.Errorf("table row has %d columns, want %d", len(row), len(headers))
		}
		_, _ = fmt.Fprintln(tw, strings.Join(safeRow(row), "\t"))
	}
	if err := tw.Flush(); err != nil {
		return "", err
	}
	return b.String(), nil
}
