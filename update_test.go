package main

import (
	"strings"
	"testing"
)

func TestInstallMethod(t *testing.T) {
	cases := map[string]string{
		"/opt/homebrew/Cellar/tsp/0.1.0/bin/tsp":                           "brew",
		"/opt/homebrew/Caskroom/tsp/0.1.0/tsp":                             "brew",
		"/usr/local/Homebrew/bin/tsp":                                      "brew",
		"/Users/me/.npm/_npx/abc/node_modules/.bin/tsp":                    "npm",
		"/Users/me/proj/node_modules/tailscale-proxy-darwin-arm64/bin/tsp": "npm",
		"/Users/me/bin/tsp":                                                "standalone",
		"/usr/local/bin/tsp":                                               "standalone",
	}
	for path, want := range cases {
		if got := installMethod(path); got != want {
			t.Errorf("installMethod(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestNormalizeVer(t *testing.T) {
	if normalizeVer("v0.1.0") != normalizeVer("0.1.0") {
		t.Error("v-prefix should be ignored")
	}
	if normalizeVer(" 0.2.0 ") != "0.2.0" {
		t.Error("whitespace should be trimmed")
	}
}

func TestReleaseArchiveURL(t *testing.T) {
	got := releaseArchiveURL("v0.2.0", "darwin", "arm64")
	want := "https://github.com/meabed/tailscale-proxy/releases/download/v0.2.0/tsp_darwin_arm64.tar.gz"
	if got != want {
		t.Errorf("got %q", got)
	}
	if got := releaseArchiveURL("v0.2.0", "windows", "amd64"); !strings.HasSuffix(got, "tsp_windows_amd64.zip") {
		t.Errorf("windows should be .zip, got %q", got)
	}
}
