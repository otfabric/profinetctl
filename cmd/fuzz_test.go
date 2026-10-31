// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/otfabric/go-profinet/gsdml"
)

// FuzzInspectGSDML feeds `profinetctl gsdml inspect` a file from elsewhere.
// A document that parses must render as JSON.
func FuzzInspectGSDML(f *testing.F) {
	f.Add([]byte(`<?xml version="1.0"?><ISO15745Profile xmlns="http://www.profibus.com/GSDML/2003/11/DeviceProfile"><ProfileBody/></ISO15745Profile>`))
	f.Add([]byte(`<ISO15745Profile schemaLocation="GSDML-DeviceProfile-V2.44.xsd"/>`))
	f.Add([]byte(`<`))
	f.Fuzz(func(t *testing.T, b []byte) {
		doc, err := gsdml.Parse(bytes.NewReader(b))
		if err != nil {
			return
		}
		if _, err := json.Marshal(inspect("fuzz.xml", doc)); err != nil {
			t.Fatalf("%q: %v", b, err)
		}
	})
}

// FuzzParseHex: the octets survive a round trip.
func FuzzParseHex(f *testing.F) {
	for _, s := range []string{"0a0b", "0a 0b", "0a:0b", "0a-0b", "0x0a0b", "0", "zz", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		b, err := parseHex(s)
		if err != nil {
			return
		}
		again, err := parseHex(hex.EncodeToString(b))
		if err != nil || !bytes.Equal(b, again) {
			t.Fatalf("%q: % x, then % x, %v", s, b, again, err)
		}
	})
}

// FuzzParseTopology feeds the --link and --peer syntaxes: what parses names
// its stations.
func FuzzParseTopology(f *testing.F) {
	for _, s := range []string{"a/port-001=b/port-002", "1=dev/port-001", "=", "/=/", "256=a/b", "a/b/c=d/e"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if links, err := parseLinks([]string{s}); err == nil && (len(links) != 1 || links[0].A.Station == "" || links[0].B.Port == "") {
			t.Fatalf("%q: %+v", s, links)
		}
		if peers, err := parsePeerChecks([]string{s}); err == nil && len(peers) != 1 {
			t.Fatalf("%q: %+v", s, peers)
		}
		_, _ = parseUint16(s)
	})
}
