// SPDX-License-Identifier: MIT

// Package buildinfo is the build metadata of the binary, as the version command prints
// it: the values the release build injects with -ldflags -X main.*, completed from the
// module and VCS information Go embeds (runtime/debug.ReadBuildInfo).
package buildinfo

import (
	"encoding/json"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
)

// Info is the build metadata. Every field is set: an unknown value is "dev" (Version),
// "unknown" (Commit, Built) or "" (Tag only); Libraries is empty, not nil, without any.
type Info struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Tag       string    `json:"tag"`
	Commit    string    `json:"commit"`
	Built     string    `json:"built"`
	Libraries []Library `json:"libraries"`
	Go        string    `json:"go"`
	Platform  string    `json:"platform"`
}

// Library is an OT Fabric library (a github.com/otfabric/go-* module) the binary is
// built with.
type Library struct {
	Name    string `json:"name"`    // go-canbus
	Version string `json:"version"` // 0.8.0, without the leading v
}

// libraryPrefix is the module path prefix of the OT Fabric libraries.
const libraryPrefix = "github.com/otfabric/"

// injected holds the values main passes to Set (from -ldflags -X main.*).
var injected struct {
	version, tag, commit, buildDate string
}

// readBuildInfo is debug.ReadBuildInfo; a test replaces it.
var readBuildInfo = debug.ReadBuildInfo

// Set records the values injected at build time. Empty values and the placeholders
// "dev", "none" and "unknown" count as not injected.
func Set(version, tag, commit, buildDate string) {
	injected.version, injected.tag, injected.commit, injected.buildDate = version, tag, commit, buildDate
}

// Get returns the build metadata of the binary called name.
func Get(name string) Info {
	info := Info{
		Name:      name,
		Version:   strings.TrimPrefix(clean(injected.version), "v"),
		Tag:       clean(injected.tag),
		Commit:    clean(injected.commit),
		Built:     clean(injected.buildDate),
		Libraries: []Library{},
		Go:        runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
	if bi, ok := readBuildInfo(); ok && bi != nil {
		if info.Version == "" && realVersion(bi.Main.Version) {
			info.Version = strings.TrimPrefix(bi.Main.Version, "v")
		}
		if info.Commit == "" {
			info.Commit = vcsCommit(bi.Settings)
		}
		info.Libraries = libraries(bi)
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	if info.Built == "" {
		info.Built = "unknown"
	}
	return info
}

// String is the one-line text form:
//
//	<name> <version> (tag <tag>, commit <commit>, built <built>, <library> <version>, …, <go>, <goos>/<goarch>)
//
// The "tag <tag>, " part is left out when there is no tag, the libraries when there are
// none.
func (i Info) String() string {
	var b strings.Builder
	b.WriteString(i.Name + " " + i.Version + " (")
	if i.Tag != "" {
		b.WriteString("tag " + i.Tag + ", ")
	}
	b.WriteString("commit " + i.Commit + ", built " + i.Built + ", ")
	for _, l := range i.Libraries {
		b.WriteString(l.Name + " " + l.Version + ", ")
	}
	b.WriteString(i.Go + ", " + i.Platform + ")")
	return b.String()
}

// JSON is the one-line JSON form, keys in the order name, version, tag, commit, built,
// libraries, go, platform.
func (i Info) JSON() string {
	if i.Libraries == nil {
		i.Libraries = []Library{}
	}
	data, err := json.Marshal(i)
	if err != nil { // a struct of strings always encodes
		panic(err)
	}
	return string(data)
}

// libraries returns the github.com/otfabric/go-* modules of bi other than the main
// module, by name: the module path after github.com/otfabric/ (go-canbus, or
// go-ieee20305/tlsccm for a nested module), at the version of its replacement if it
// has one.
func libraries(bi *debug.BuildInfo) []Library {
	out := []Library{}
	for _, d := range bi.Deps {
		if d == nil || d.Path == bi.Main.Path || !strings.HasPrefix(d.Path, libraryPrefix+"go-") {
			continue
		}
		version := d.Version
		if d.Replace != nil {
			version = d.Replace.Version
		}
		if version = strings.TrimPrefix(version, "v"); version == "" {
			version = "unknown"
		}
		out = append(out, Library{Name: strings.TrimPrefix(d.Path, libraryPrefix), Version: version})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// pseudoVersion matches the timestamp and commit of a pseudo-version
// (v0.4.1-0.20261107212139-415629327cbc).
var pseudoVersion = regexp.MustCompile(`[-.]\d{14}-[0-9a-f]{12}(\+[0-9A-Za-z.-]+)?$`)

// realVersion reports whether v, the module version Go embeds, names a release: not
// "(devel)", not a pseudo-version and not the "+dirty" version Go stamps on a build of
// a modified checkout. go install …@vX.Y.Z embeds vX.Y.Z.
func realVersion(v string) bool {
	v = clean(v)
	return v != "" && v != "(devel)" && !strings.HasSuffix(v, "+dirty") && !pseudoVersion.MatchString(v)
}

// vcsCommit is vcs.revision, with "-dirty" when vcs.modified is true, or "".
func vcsCommit(settings []debug.BuildSetting) string {
	var rev string
	var dirty bool
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return ""
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}

// clean trims s and maps the placeholders of an uninjected value to "".
func clean(s string) string {
	s = strings.TrimSpace(s)
	switch s {
	case "dev", "none", "unknown":
		return ""
	}
	return s
}
