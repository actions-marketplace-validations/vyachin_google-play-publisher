package version_test

import (
	"testing"

	"github.com/vyachin/google-play-publisher/internal/version"
)

func TestInfoString(t *testing.T) {
	info := version.Info{
		Version: "1.2.3",
		Commit:  "abc1234",
		Date:    "2026-11-09",
		Go:      "go1.27",
	}

	want := "1.2.3 (commit abc1234, built 2026-11-09, go1.27)"
	if got := info.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestNew(t *testing.T) {
	info := version.New()
	if info.Version != version.Version {
		t.Errorf("Version = %q, want %q", info.Version, version.Version)
	}
	if info.Go == "" {
		t.Error("Go version is empty")
	}
}
