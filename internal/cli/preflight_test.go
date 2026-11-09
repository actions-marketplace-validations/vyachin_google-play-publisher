package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vyachin/google-play-publisher/internal/cli"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
	"github.com/vyachin/google-play-publisher/internal/release"
	"github.com/vyachin/google-play-publisher/internal/version"
)

func TestPublishLocalValidationBeforeClient(t *testing.T) {
	aab := writeAAB(t)

	notAAB := filepath.Join(t.TempDir(), "not-a-bundle.aab")
	if err := os.WriteFile(notAAB, []byte("not a zip archive"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	changelogDir := t.TempDir()
	writeChangelog(t, changelogDir, "en-US", 42, strings.Repeat("x", release.MaxNoteLength+1))

	tests := []struct {
		name string
		args []string
	}{
		{"invalid package", []string{"publish", "-p", "com", "-f", aab}},
		{"invalid track", []string{"publish", "-p", "com.example.app", "-f", aab, "-t", "internal!"}},
		{"invalid status", []string{"publish", "-p", "com.example.app", "-f", aab, "--status", "beta"}},
		{"rollout out of range", []string{"publish", "-p", "com.example.app", "-f", aab, "--rollout", "2"}},
		{"NaN rollout", []string{"publish", "-p", "com.example.app", "-f", aab, "--rollout", "NaN"}},
		{"invalid AAB", []string{"publish", "-p", "com.example.app", "-f", notAAB}},
		{"package does not match AAB", []string{"publish", "-p", "com.example.other", "-f", aab}},
		{"version code does not match AAB", []string{
			"publish", "-p", "com.example.app", "-f", aab, "--version-code", "41",
		}},
		{"metadata mismatch", []string{
			"publish", "-p", "com.example.other", "-f", aab, "--metadata", writeMetadata(t, 42),
		}},
		{"metadata version conflict", []string{
			"publish", "-p", "com.example.app", "-f", aab,
			"--metadata", writeMetadata(t, 41), "--version-code", "42", "--if-exists", "skip",
		}},
		{"too long changelog", []string{
			"publish", "-p", "com.example.app", "-f", aab,
			"--changelog-dir", changelogDir, "--version-code", "42",
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			factoryCalls := 0
			app := &cli.App{
				Stdout:  &strings.Builder{},
				Stderr:  &strings.Builder{},
				Now:     time.Now,
				Version: version.Info{Version: "test"},
				NewAPI: func(context.Context, play.Options) (play.API, error) {
					factoryCalls++
					return nil, exitcode.Errorf(exitcode.Auth, "credentials are unavailable")
				},
			}

			code := cli.Execute(context.Background(), app, test.args)
			if code != int(exitcode.Usage) {
				t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.Usage, app.Stderr)
			}
			if factoryCalls != 0 {
				t.Fatalf("API factory called %d times, want 0", factoryCalls)
			}
		})
	}
}

func TestDoctorMetadataRequiresAABBeforeClient(t *testing.T) {
	factoryCalls := 0
	app := &cli.App{
		Stdout: &strings.Builder{}, Stderr: &strings.Builder{},
		NewAPI: func(context.Context, play.Options) (play.API, error) {
			factoryCalls++
			return nil, exitcode.Errorf(exitcode.Auth, "credentials are unavailable")
		},
	}
	code := cli.Execute(context.Background(), app, []string{
		"doctor", "-p", "com.example.app", "--metadata", "missing-metadata.json", "--json",
	})
	if code != int(exitcode.Usage) || factoryCalls != 0 {
		t.Fatalf("code = %d, factory calls = %d: %s", code, factoryCalls, app.Stderr)
	}
	if !strings.Contains(app.Stdout.(*strings.Builder).String(), "--metadata requires --aab") {
		t.Fatalf("missing metadata diagnostic: %s", app.Stdout)
	}
}

func TestPublishValidInputBuildsClientOnce(t *testing.T) {
	factoryCalls := 0
	app := &cli.App{
		Stdout:  &strings.Builder{},
		Stderr:  &strings.Builder{},
		Now:     time.Now,
		Version: version.Info{Version: "test"},
		NewAPI: func(context.Context, play.Options) (play.API, error) {
			factoryCalls++
			return &playtest.Fake{}, nil
		},
	}

	code := cli.Execute(context.Background(), app, []string{
		"publish", "-p", "com.example.app", "-f", writeAAB(t),
	})
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, app.Stderr)
	}
	if factoryCalls != 1 {
		t.Fatalf("API factory called %d times, want 1", factoryCalls)
	}
}
