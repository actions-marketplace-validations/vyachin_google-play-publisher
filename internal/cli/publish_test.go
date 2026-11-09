package cli_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vyachin/google-play-publisher/internal/artifact/artifacttest"
	"github.com/vyachin/google-play-publisher/internal/cli"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
	"github.com/vyachin/google-play-publisher/internal/version"
)

func runWithAPI(t *testing.T, api play.API, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errOut bytes.Buffer
	app := &cli.App{
		Stdout:  &out,
		Stderr:  &errOut,
		Now:     time.Now,
		Version: version.Info{Version: "test"},
		NewAPI: func(context.Context, play.Options) (play.API, error) {
			return api, nil
		},
	}

	code = cli.Execute(context.Background(), app, args)
	return out.String(), errOut.String(), code
}

func writeAAB(t *testing.T) string {
	return writeBundleFiles(t, map[string]string{
		"BundleConfig.pb":                   "bundle-config",
		"base/manifest/AndroidManifest.xml": string(artifacttest.Manifest("com.example.app", 42, "1.0.42")),
	})
}

func writeBundleFiles(t *testing.T, files map[string]string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app-release.aab")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create AAB: %v", err)
	}

	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	return path
}

func writeMetadata(t *testing.T, versionCode int64) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "output-metadata.json")
	body := `{"artifactType":{"type":"BUNDLE"},"applicationId":"com.example.app",` +
		`"elements":[{"type":"SINGLE","versionCode":` + strconv.FormatInt(versionCode, 10) + `}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	return path
}

func TestPublishJSON(t *testing.T) {
	out, errOut, code := runWithAPI(t, &playtest.Fake{}, "--json", "publish",
		"-p", "com.example.app", "-f", writeAAB(t), "-t", "internal")

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out)
	}
	if got["ok"] != true || got["review"] != "sent" || got["track"] != "internal" {
		t.Fatalf("unexpected JSON: %#v", got)
	}
	if got["versionCode"] != float64(42) {
		t.Fatalf("versionCode = %v", got["versionCode"])
	}
	if got["schemaVersion"] != float64(1) {
		t.Fatalf("schemaVersion = %v, want 1", got["schemaVersion"])
	}
	if got["versionName"] != "1.0.42" {
		t.Fatalf("versionName = %v, want 1.0.42", got["versionName"])
	}
}

func TestPublishPackageFromAAB(t *testing.T) {
	out, errOut, code := runWithAPI(t, &playtest.Fake{}, "--json", "publish",
		"-f", writeAAB(t))

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out)
	}
	if got["package"] != "com.example.app" {
		t.Fatalf("package = %v, want the AAB applicationId", got["package"])
	}
}

func TestPublishPackageFromMetadata(t *testing.T) {
	out, errOut, code := runWithAPI(t, &playtest.Fake{}, "--json", "publish",
		"-f", writeAAB(t), "--metadata", writeMetadata(t, 42))

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out)
	}
	if got["package"] != "com.example.app" {
		t.Fatalf("package = %v, want the metadata applicationId", got["package"])
	}
}

func TestPublishHumanOutput(t *testing.T) {
	out, errOut, code := runWithAPI(t, &playtest.Fake{}, "publish",
		"-p", "com.example.app", "-f", writeAAB(t))

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}
	if !strings.Contains(out, "published com.example.app version 42 to internal") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestPublishDryRunJSON(t *testing.T) {
	fake := &playtest.Fake{}
	out, errOut, code := runWithAPI(t, fake, "--json", "publish",
		"-p", "com.example.app", "-f", writeAAB(t), "--dry-run")

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out)
	}
	if got["review"] != "validated" {
		t.Fatalf("review = %v, want validated", got["review"])
	}
	if !contains(fake.Calls, "DeleteEdit") {
		t.Fatalf("dry run must delete the edit: %v", fake.Calls)
	}
}

func TestPublishSkipJSON(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return []play.ReleaseSummary{{VersionCodes: []int64{42}}}, nil
		},
	}
	out, errOut, code := runWithAPI(t, fake, "--json", "publish",
		"-p", "com.example.app", "-f", writeAAB(t),
		"--metadata", writeMetadata(t, 42), "--if-exists", "skip")

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out)
	}
	if got["review"] != "skipped" {
		t.Fatalf("review = %v, want skipped", got["review"])
	}
}

func TestPublishRequiresFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no package", args: []string{"publish", "-f", "app.aab"}},
		{name: "no aab", args: []string{"publish", "-p", "com.example.app"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, code := runWithAPI(t, &playtest.Fake{}, tt.args...)
			if code != int(exitcode.Usage) {
				t.Fatalf("code = %d, want %d", code, exitcode.Usage)
			}
		})
	}
}

func TestPublishInvalidOnInReview(t *testing.T) {
	_, errOut, code := runWithAPI(t, &playtest.Fake{}, "publish",
		"-p", "com.example.app", "-f", writeAAB(t), "--on-in-review", "bogus")

	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d", code, exitcode.Usage)
	}
	if !strings.Contains(errOut, "on-in-review") {
		t.Fatalf("unexpected stderr: %q", errOut)
	}
}

func TestPublishInvalidRollout(t *testing.T) {
	_, _, code := runWithAPI(t, &playtest.Fake{}, "publish",
		"-p", "com.example.app", "-f", writeAAB(t), "--rollout", "1")

	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d", code, exitcode.Usage)
	}
}

func TestPublishNotSentForReviewJSON(t *testing.T) {
	out, errOut, code := runWithAPI(t, &playtest.Fake{}, "--json", "publish",
		"-p", "com.example.app", "-f", writeAAB(t), "--changes-not-sent-for-review")

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out)
	}
	if got["review"] != "notSent" {
		t.Fatalf("review = %v, want notSent", got["review"])
	}
}

func TestPublishRolloutOnly(t *testing.T) {
	out, errOut, code := runWithAPI(t, &playtest.Fake{}, "--json", "publish",
		"-p", "com.example.app", "-f", writeAAB(t), "--rollout", "0.2")

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out)
	}
	if got["status"] != "inProgress" {
		t.Fatalf("status = %v, want inProgress", got["status"])
	}
}

func TestPublishIncompatibleFlags(t *testing.T) {
	_, errOut, code := runWithAPI(t, &playtest.Fake{}, "publish",
		"-p", "com.example.app", "-f", writeAAB(t),
		"--dry-run", "--changes-not-sent-for-review")

	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d", code, exitcode.Usage)
	}
	if !strings.Contains(errOut, "cannot be combined") {
		t.Fatalf("unexpected stderr: %q", errOut)
	}
}

func writeChangelog(t *testing.T, root, locale string, versionCode int64, text string) {
	t.Helper()

	dir := filepath.Join(root, locale, "changelogs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create changelog dir: %v", err)
	}
	path := filepath.Join(dir, strconv.FormatInt(versionCode, 10)+".txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("write changelog: %v", err)
	}
}

func TestPublishRejectsReleaseNotesFlag(t *testing.T) {
	_, errOut, code := runWithAPI(t, &playtest.Fake{}, "publish",
		"-p", "com.example.app", "-f", writeAAB(t), "--release-notes", "notes.json")

	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d", code, exitcode.Usage)
	}
	if !strings.Contains(errOut, "unknown flag") {
		t.Fatalf("unexpected stderr: %q", errOut)
	}
}

func TestPublishChangelogDirUsesAABVersionCode(t *testing.T) {
	root := t.TempDir()
	writeChangelog(t, root, "en-US", 42, "Bug fixes")

	fake := &playtest.Fake{}
	_, errOut, code := runWithAPI(t, fake, "publish",
		"-p", "com.example.app", "-f", writeAAB(t), "--changelog-dir", root)

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}
	if len(fake.UpdatedReleases) != 1 || len(fake.UpdatedReleases[0].ReleaseNotes) != 1 {
		t.Fatalf("release notes were not attached: %#v", fake.UpdatedReleases)
	}
}

func TestPublishMissingChangelog(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "ru-RU", "changelogs"), 0o755); err != nil {
		t.Fatalf("create changelog dir: %v", err)
	}

	_, errOut, code := runWithAPI(t, &playtest.Fake{}, "publish",
		"-p", "com.example.app", "-f", writeAAB(t),
		"--changelog-dir", root, "--version-code", "42")

	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d", code, exitcode.Usage)
	}
	if !strings.Contains(errOut, "no changelog for versionCode 42") || !strings.Contains(errOut, "42.txt") {
		t.Fatalf("unexpected stderr: %q", errOut)
	}
}

func TestPublishChangelogNotes(t *testing.T) {
	root := t.TempDir()
	writeChangelog(t, root, "ru-RU", 42, "Исправления ошибок")
	writeChangelog(t, root, "en-US", 42, "Bug fixes")

	fake := &playtest.Fake{}
	_, errOut, code := runWithAPI(t, fake, "publish",
		"-p", "com.example.app", "-f", writeAAB(t),
		"--changelog-dir", root, "--version-code", "42")

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d, stderr: %q", code, exitcode.OK, errOut)
	}
	if len(fake.UpdatedReleases) != 1 {
		t.Fatalf("track updates = %d, want 1", len(fake.UpdatedReleases))
	}

	got := fake.UpdatedReleases[0].ReleaseNotes
	if len(got) != 2 {
		t.Fatalf("release notes = %#v, want 2 locales", got)
	}
	if got[0].Language != "en-US" || got[0].Text != "Bug fixes" {
		t.Fatalf("first note = %#v, want en-US", got[0])
	}
	if got[1].Language != "ru-RU" || got[1].Text != "Исправления ошибок" {
		t.Fatalf("second note = %#v, want ru-RU", got[1])
	}
}

func TestPublishVersionCodeMismatch(t *testing.T) {
	root := t.TempDir()
	writeChangelog(t, root, "ru-RU", 42, "Исправления ошибок")

	fake := &playtest.Fake{
		UploadBundleFunc: func(context.Context, string, string, io.Reader, int64) (play.BundleInfo, error) {
			return play.BundleInfo{VersionCode: 43}, nil
		},
	}

	_, errOut, code := runWithAPI(t, fake, "publish",
		"-p", "com.example.app", "-f", writeAAB(t),
		"--changelog-dir", root, "--version-code", "42")

	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d", code, exitcode.Usage)
	}
	if !strings.Contains(errOut, "does not match uploaded versionCode 43") {
		t.Fatalf("unexpected stderr: %q", errOut)
	}
	if len(fake.DeletedEditIDs) == 0 {
		t.Fatal("edit was not cleaned up after the version mismatch")
	}
}

func TestPublishMissingAABFile(t *testing.T) {
	_, _, code := runWithAPI(t, &playtest.Fake{}, "publish",
		"-p", "com.example.app", "-f", filepath.Join(t.TempDir(), "missing.aab"))

	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d", code, exitcode.Usage)
	}
}

func TestPublishAPIErrorExitCode(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return nil, exitcode.Errorf(exitcode.Auth, "permission denied")
		},
	}
	_, errOut, code := runWithAPI(t, fake, "publish",
		"-p", "com.example.app", "-f", writeAAB(t))

	if code != int(exitcode.Auth) {
		t.Fatalf("code = %d, want %d", code, exitcode.Auth)
	}
	if !strings.Contains(errOut, "permission denied") {
		t.Fatalf("unexpected stderr: %q", errOut)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
