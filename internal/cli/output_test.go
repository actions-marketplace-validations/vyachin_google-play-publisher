package cli_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/vyachin/google-play-publisher/internal/cli"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
	"github.com/vyachin/google-play-publisher/internal/version"
)

func runTextWithWriter(api play.API, w io.Writer, args ...string) (string, int) {
	var errOut bytes.Buffer
	app := &cli.App{
		Stdout:  w,
		Stderr:  &errOut,
		Now:     time.Now,
		Version: version.Info{Version: "test"},
		NewAPI: func(context.Context, play.Options) (play.API, error) {
			return api, nil
		},
	}
	code := cli.Execute(context.Background(), app, args)
	return errOut.String(), code
}

func TestPublishTextWriteError(t *testing.T) {
	stderr, code := runTextWithWriter(&playtest.Fake{}, errWriter{}, "publish",
		"-p", "com.example.app", "-f", writeAAB(t))

	if code != int(exitcode.Internal) {
		t.Fatalf("code = %d, want %d", code, exitcode.Internal)
	}
	if !strings.Contains(stderr, "google-play-publisher:") {
		t.Fatalf("expected a diagnostic on stderr: %q", stderr)
	}
}

func TestPublishTextManualActionWriteErrorKeepsCode(t *testing.T) {
	attempts := 0
	fake := &playtest.Fake{
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			attempts++
			if attempts == 1 {
				return exitcode.Errorf(exitcode.ManualAction,
					"changes cannot be sent for review automatically")
			}
			return nil
		},
	}

	stderr, code := runTextWithWriter(fake, errWriter{}, "publish",
		"-p", "com.example.app", "-f", writeAAB(t))

	if code != int(exitcode.ManualAction) {
		t.Fatalf("code = %d, want %d: the saved release still needs submission", code, exitcode.ManualAction)
	}
	if !strings.Contains(stderr, "changes saved but not submitted for review") {
		t.Fatalf("saved-release context is lost: %q", stderr)
	}
	if !strings.Contains(stderr, "writing the result failed") {
		t.Fatalf("output failure is not reported: %q", stderr)
	}
}

func TestStatusTextWriteError(t *testing.T) {
	stderr, code := runTextWithWriter(&playtest.Fake{}, errWriter{}, "status",
		"-p", "com.example.app")

	if code != int(exitcode.Internal) {
		t.Fatalf("code = %d, want %d", code, exitcode.Internal)
	}
	if !strings.Contains(stderr, "google-play-publisher:") {
		t.Fatalf("expected a diagnostic on stderr: %q", stderr)
	}
}

func TestDoctorTextWriteError(t *testing.T) {
	stderr, code := runTextWithWriter(&playtest.Fake{}, errWriter{}, "doctor",
		"-p", "com.example.app")

	if code != int(exitcode.Internal) {
		t.Fatalf("code = %d, want %d", code, exitcode.Internal)
	}
	if !strings.Contains(stderr, "google-play-publisher:") {
		t.Fatalf("expected a diagnostic on stderr: %q", stderr)
	}
}

func TestDoctorTextCheckErrorWinsOverWriteError(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return nil, exitcode.Errorf(exitcode.Auth, "permission denied")
		},
	}

	stderr, code := runTextWithWriter(fake, errWriter{}, "doctor", "-p", "com.example.app")

	if code != int(exitcode.Auth) {
		t.Fatalf("code = %d, want %d: the check diagnosis must win", code, exitcode.Auth)
	}
	if !strings.Contains(stderr, "permission denied") {
		t.Fatalf("check failure is lost: %q", stderr)
	}
	if !strings.Contains(stderr, "check output could not be written") {
		t.Fatalf("output failure is not reported: %q", stderr)
	}
}
