// SPDX-License-Identifier: MIT

package buildinfo

import (
	"runtime"
	"runtime/debug"
	"testing"
)

const sha = "1802d0f9afe8642d1e278120b76fcdfac967cb9e"

// with sets the injected values and the embedded build info for one test.
func with(t *testing.T, version, tag, commit, buildDate string, bi *debug.BuildInfo) {
	t.Helper()
	oldInjected, oldRead := injected, readBuildInfo
	t.Cleanup(func() { injected, readBuildInfo = oldInjected, oldRead })
	Set(version, tag, commit, buildDate)
	readBuildInfo = func() (*debug.BuildInfo, bool) { return bi, bi != nil }
}

func platform() string { return runtime.Version() + ", " + runtime.GOOS + "/" + runtime.GOARCH }

func TestStringAllFields(t *testing.T) {
	with(t, "3.5.1", "v3.5.1", sha, "2026-11-05T15:41:49Z", nil)
	want := "tool 3.5.1 (tag v3.5.1, commit " + sha + ", built 2026-11-05T15:41:49Z, " + platform() + ")"
	if got := Get("tool").String(); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestStringWithoutTag(t *testing.T) {
	for _, tag := range []string{"", "none"} {
		with(t, "v3.5.1-2-gabcdef", tag, sha, "2026-11-05T15:41:49Z", nil)
		want := "tool 3.5.1-2-gabcdef (commit " + sha + ", built 2026-11-05T15:41:49Z, " + platform() + ")"
		if got := Get("tool").String(); got != want {
			t.Fatalf("tag %q: got  %q\nwant %q", tag, got, want)
		}
	}
}

func TestDevFallbackWithoutBuildInfo(t *testing.T) {
	with(t, "", "", "", "", nil)
	want := "tool dev (commit unknown, built unknown, " + platform() + ")"
	if got := Get("tool").String(); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestDevFallbackPlaceholders(t *testing.T) {
	with(t, "dev", "none", "unknown", "unknown", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}})
	want := "tool dev (commit unknown, built unknown, " + platform() + ")"
	if got := Get("tool").String(); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestDirtyCheckoutVersionIsDev(t *testing.T) {
	bi := &debug.BuildInfo{
		Main:     debug.Module{Version: "v0.4.1-0.20261107212139-415629327cbc+dirty"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: sha}, {Key: "vcs.modified", Value: "true"}},
	}
	with(t, "", "", "", "", bi)
	want := "tool dev (commit " + sha + "-dirty, built unknown, " + platform() + ")"
	if got := Get("tool").String(); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestFallbackToEmbeddedBuildInfo(t *testing.T) {
	bi := &debug.BuildInfo{
		Main:     debug.Module{Version: "v3.5.1"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: sha}, {Key: "vcs.modified", Value: "true"}},
	}
	with(t, "", "", "", "", bi)
	want := "tool 3.5.1 (commit " + sha + "-dirty, built unknown, " + platform() + ")"
	if got := Get("tool").String(); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	bi.Settings[1].Value = "false"
	if got := Get("tool").Commit; got != sha {
		t.Fatalf("clean tree: commit %q, want %q", got, sha)
	}
}

func TestInjectedWinsOverBuildInfo(t *testing.T) {
	bi := &debug.BuildInfo{
		Main:     debug.Module{Version: "v0.0.1"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "0000"}},
	}
	with(t, "v3.5.1", "v3.5.1", sha, "2026-11-05T15:41:49Z", bi)
	i := Get("tool")
	if i.Version != "3.5.1" || i.Commit != sha {
		t.Fatalf("got %+v", i)
	}
}

func TestJSONKeyOrderAndValues(t *testing.T) {
	with(t, "3.5.1", "v3.5.1", sha, "2026-11-05T15:41:49Z", nil)
	want := `{"name":"tool","version":"3.5.1","tag":"v3.5.1","commit":"` + sha +
		`","built":"2026-11-05T15:41:49Z","libraries":[],"go":"` + runtime.Version() + `","platform":"` + runtime.GOOS + "/" + runtime.GOARCH + `"}`
	if got := Get("tool").JSON(); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestJSONDevBuild(t *testing.T) {
	with(t, "", "", "", "", nil)
	want := `{"name":"tool","version":"dev","tag":"","commit":"unknown","built":"unknown","libraries":[],"go":"` +
		runtime.Version() + `","platform":"` + runtime.GOOS + "/" + runtime.GOARCH + `"}`
	if got := Get("tool").JSON(); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestPseudoVersionIsDev(t *testing.T) {
	for _, v := range []string{
		"v0.4.1-0.20261107212139-415629327cbc",
		"v0.0.0-20261107212139-415629327cbc",
		"v1.2.0-rc.1.0.20261107212139-415629327cbc",
		"v0.4.1-0.20261107212139-415629327cbc+dirty",
		"v0.4.0+dirty",
	} {
		with(t, "", "", "", "", &debug.BuildInfo{Main: debug.Module{Version: v}})
		if got := Get("tool").Version; got != "dev" {
			t.Errorf("%s: version %q, want dev", v, got)
		}
	}
	with(t, "", "", "", "", &debug.BuildInfo{Main: debug.Module{Version: "v1.2.0-rc.1"}})
	if got := Get("tool").Version; got != "1.2.0-rc.1" {
		t.Errorf("v1.2.0-rc.1: version %q", got)
	}
}

// info is a release build with the libraries libs, independent of the real build info.
func info(libs ...Library) Info {
	return Info{Name: "tool", Version: "0.6.0", Tag: "v0.6.0", Commit: sha, Built: "2026-11-05T15:41:49Z",
		Libraries: libs, Go: "go1.27.3", Platform: "darwin/arm64"}
}

func TestLibrariesText(t *testing.T) {
	head := "tool 0.6.0 (tag v0.6.0, commit " + sha + ", built 2026-11-05T15:41:49Z, "
	for _, c := range []struct {
		libs []Library
		want string
	}{
		{nil, head + "go1.27.3, darwin/arm64)"},
		{[]Library{{"go-canbus", "0.8.0"}}, head + "go-canbus 0.8.0, go1.27.3, darwin/arm64)"},
		{[]Library{{"go-canbus", "0.8.0"}, {"go-canopen", "0.5.1"}}, head + "go-canbus 0.8.0, go-canopen 0.5.1, go1.27.3, darwin/arm64)"},
	} {
		if got := info(c.libs...).String(); got != c.want {
			t.Errorf("got  %q\nwant %q", got, c.want)
		}
	}
}

func TestLibrariesJSON(t *testing.T) {
	head := `{"name":"tool","version":"0.6.0","tag":"v0.6.0","commit":"` + sha + `","built":"2026-11-05T15:41:49Z","libraries":`
	tail := `,"go":"go1.27.3","platform":"darwin/arm64"}`
	for _, c := range []struct {
		libs []Library
		want string
	}{
		{nil, head + "[]" + tail},
		{[]Library{{"go-canbus", "0.8.0"}}, head + `[{"name":"go-canbus","version":"0.8.0"}]` + tail},
		{[]Library{{"go-canbus", "0.8.0"}, {"go-canopen", "0.5.1"}},
			head + `[{"name":"go-canbus","version":"0.8.0"},{"name":"go-canopen","version":"0.5.1"}]` + tail},
	} {
		if got := info(c.libs...).JSON(); got != c.want {
			t.Errorf("got  %s\nwant %s", got, c.want)
		}
	}
}

func TestLibrariesFromBuildInfo(t *testing.T) {
	bi := &debug.BuildInfo{
		Main: debug.Module{Path: "github.com/otfabric/go-canopen/cmd/canopenctl"},
		Deps: []*debug.Module{
			{Path: "github.com/otfabric/go-canopen", Version: "v0.5.1"},
			{Path: "github.com/otfabric/go-canopen/cmd/canopenctl", Version: "v9.9.9"},
			{Path: "github.com/spf13/cobra", Version: "v1.10.2"},
			{Path: "github.com/otfabric/docsync", Version: "v0.3.1"},
			{Path: "github.com/otfabric/go-canbus", Version: "v0.8.0", Replace: &debug.Module{Path: "../go-canbus"}},
			{Path: "github.com/otfabric/go-ieee20305/tlsccm", Version: "v0.3.0"},
			{Path: "github.com/otfabric/go-ieee20305", Version: "v0.6.1-0.20261107212139-415629327cbc",
				Replace: &debug.Module{Path: "github.com/otfabric/go-ieee20305", Version: "v0.6.2"}},
		},
	}
	with(t, "", "", "", "", bi)
	got := Get("tool").Libraries
	want := []Library{{"go-canbus", "unknown"}, {"go-canopen", "0.5.1"}, {"go-ieee20305", "0.6.2"}, {"go-ieee20305/tlsccm", "0.3.0"}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
