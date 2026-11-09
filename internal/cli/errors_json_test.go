package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// decodeSingleObject decodes exactly one JSON object from out.
func decodeSingleObject(t *testing.T, out string) map[string]any {
	t.Helper()

	dec := json.NewDecoder(strings.NewReader(out))
	var got map[string]any
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("stdout is not a JSON object: %v (%q)", err, out)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout must contain exactly one JSON object, got extra content %v", extra)
	}
	return got
}

func errorObject(t *testing.T, got map[string]any) map[string]any {
	t.Helper()

	if ok, _ := got["ok"].(bool); ok {
		t.Fatalf("ok = true, want false: %v", got)
	}
	detail, ok := got["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error object: %v", got)
	}
	return detail
}

func TestPublishJSONUsageError(t *testing.T) {
	out, errOut, code := runWithAPI(t, &playtest.Fake{}, "--json", "publish")

	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d", code, exitcode.Usage)
	}

	got := decodeSingleObject(t, out)
	if got["command"] != "publish" {
		t.Fatalf("command = %v, want publish", got["command"])
	}

	detail := errorObject(t, got)
	if detail["code"] != "usage" || detail["exitCode"] != float64(exitcode.Usage) {
		t.Fatalf("unexpected error object: %v", detail)
	}
	if !strings.Contains(detail["message"].(string), "--package") {
		t.Fatalf("message does not explain the problem: %v", detail["message"])
	}
	if !strings.Contains(errOut, detail["message"].(string)) || !strings.Contains(errOut, "--help") {
		t.Fatalf("usage diagnostic missing on stderr: %q", errOut)
	}
}

func TestPublishJSONAPIError(t *testing.T) {
	apiErr := exitcode.WithHint(exitcode.Wrap(exitcode.Conflict, &play.APIError{
		Status:  400,
		Reason:  "badRequest",
		Message: "version code 42 has already been used",
	}), "bump versionCode if it was already used")
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return nil, apiErr
		},
	}

	out, errOut, code := runWithAPI(t, fake, "--json", "publish",
		"-p", "com.example.app", "-f", writeAAB(t))

	if code != int(exitcode.Conflict) {
		t.Fatalf("code = %d, want %d", code, exitcode.Conflict)
	}

	detail := errorObject(t, decodeSingleObject(t, out))
	if detail["code"] != "conflict" || detail["exitCode"] != float64(exitcode.Conflict) {
		t.Fatalf("unexpected error object: %v", detail)
	}
	if detail["apiCode"] != float64(400) || detail["reason"] != "badRequest" {
		t.Fatalf("API metadata is lost: %v", detail)
	}
	if strings.Count(errOut, "google-play-publisher: ") != 1 ||
		!strings.Contains(errOut, detail["message"].(string)) ||
		!strings.Contains(errOut, "hint: "+detail["hint"].(string)) {
		t.Fatalf("expected error and hint exactly once on stderr: %q", errOut)
	}
}

func TestPublishJSONManualAction(t *testing.T) {
	attempts := 0
	fake := &playtest.Fake{
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			attempts++
			if attempts == 1 {
				return exitcode.Errorf(exitcode.ManualAction, "changes cannot be sent for review automatically")
			}
			return nil
		},
	}

	out, errOut, code := runWithAPI(t, fake, "--json", "publish",
		"-p", "com.example.app", "-f", writeAAB(t))

	if code != int(exitcode.ManualAction) {
		t.Fatalf("code = %d, want %d", code, exitcode.ManualAction)
	}

	// The saved release is a success object, but the operator still needs the
	// Play Console action on stderr, exactly once.
	if got := strings.Count(errOut, "Play Console"); got != 1 {
		t.Fatalf("operator action printed %d times, want 1: %q", got, errOut)
	}
	if !strings.Contains(errOut, "changes saved but not submitted for review") {
		t.Fatalf("manual-action reason missing on stderr: %q", errOut)
	}
	if strings.Contains(errOut, `"ok": false`) {
		t.Fatalf("stderr contains an error object: %q", errOut)
	}

	got := decodeSingleObject(t, out)
	if got["ok"] != true {
		t.Fatalf("the saved release must be reported: %v", got)
	}
	if got["review"] != "notSent" || got["versionCode"] != float64(42) || got["editId"] != "edit-1" {
		t.Fatalf("unexpected result: %v", got)
	}
}

func TestDoctorJSONErrorSingleObject(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return nil, exitcode.Errorf(exitcode.Auth, "permission denied")
		},
	}

	out, errOut, code := runWithAPI(t, fake, "--json", "doctor", "-p", "com.example.app")

	if code != int(exitcode.Auth) {
		t.Fatalf("code = %d, want %d", code, exitcode.Auth)
	}

	got := decodeSingleObject(t, out)
	if checks, ok := got["checks"].([]any); !ok || len(checks) == 0 {
		t.Fatalf("doctor checks are missing: %v", got)
	}

	detail := errorObject(t, got)
	if detail["code"] != "auth" {
		t.Fatalf("unexpected error object: %v", detail)
	}
	if strings.Count(errOut, "google-play-publisher: ") != 1 || !strings.Contains(errOut, "permission denied") {
		t.Fatalf("doctor diagnostic missing or duplicated on stderr: %q", errOut)
	}
}

func TestDoctorJSONWriteError(t *testing.T) {
	app := &cli.App{
		Stdout:  errWriter{},
		Stderr:  &bytes.Buffer{},
		Now:     time.Now,
		Version: version.Info{Version: "test"},
		NewAPI:  func(context.Context, play.Options) (play.API, error) { return &playtest.Fake{}, nil },
	}

	code := cli.Execute(context.Background(), app, []string{"--json", "doctor", "-p", "com.example.app"})
	if code == int(exitcode.OK) {
		t.Fatalf("a failed JSON write must not return 0")
	}
	if app.Stderr.(*bytes.Buffer).Len() == 0 {
		t.Fatal("expected a diagnostic on stderr")
	}
}

func TestInterruptedJSON(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	app := &cli.App{
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Now:     time.Now,
		Version: version.Info{Version: "test"},
		NewAPI: func(context.Context, play.Options) (play.API, error) {
			return &playtest.Fake{
				TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
					return nil, context.Canceled
				},
			}, nil
		},
	}

	code := cli.Execute(ctx, app, []string{"--json", "publish", "-p", "com.example.app", "-f", writeAAB(t)})
	if code != int(exitcode.Interrupted) {
		t.Fatalf("code = %d, want %d", code, exitcode.Interrupted)
	}

	detail := errorObject(t, decodeSingleObject(t, app.Stdout.(*bytes.Buffer).String()))
	if detail["code"] != "interrupted" {
		t.Fatalf("unexpected error object: %v", detail)
	}
	if !strings.Contains(app.Stderr.(*bytes.Buffer).String(), "operation interrupted") {
		t.Fatal("interruption diagnostic missing on stderr")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
