// SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"io"
	"os"
)

// maxInputFile bounds the files profinetctl reads whole: a device model
// (--model), record data (--data-file) and an expected topology
// (--expect). Real ones are a few KiB; GSDML files are bounded by
// gsdml.Parse (gsdml.MaxDocumentSize) instead.
const maxInputFile = 16 << 20

// readInputFile reads the file the operator names with a flag, refusing
// one larger than maxInputFile instead of reading it into memory.
func readInputFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return readLimited(f, maxInputFile)
}

// readLimited reads r up to limit octets; more is an error.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("larger than %d octets", limit)
	}
	return b, nil
}

// outputFileMode is the mode of the files profinetctl creates (GSDML,
// extracted package parts, pcap captures): readable by the operator only,
// since a capture holds the traffic of the network and a description
// may not be public yet. umask can only narrow it.
const outputFileMode = 0o600

// writeOutputFile writes b to the file the operator names with
// --output-file or a similar flag, replacing its contents.
func writeOutputFile(path string, b []byte) error {
	return os.WriteFile(path, b, outputFileMode)
}

// openOutputFile opens (or creates) the file the operator names for
// output written as it is produced, without emptying it: the caller
// truncates it once the output can start.
func openOutputFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE, outputFileMode)
}
