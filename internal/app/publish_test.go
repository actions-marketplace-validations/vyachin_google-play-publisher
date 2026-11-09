package app_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/artifact"
	"github.com/vyachin/google-play-publisher/internal/artifact/artifacttest"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
)

func writeAAB(t *testing.T) string {
	return writeAABWithIdentity(t, "com.example.app", 42, "1.0.42")
}

func writeAABWithIdentity(t *testing.T, pkg string, versionCode int64, versionName string) string {
	return writeBundleFiles(t, map[string]string{
		"BundleConfig.pb":                   "bundle-config",
		"base/manifest/AndroidManifest.xml": string(artifacttest.Manifest(pkg, versionCode, versionName)),
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
	body := `{
		"artifactType": {"type": "BUNDLE"},
		"applicationId": "com.example.app",
		"elements": [{"type": "SINGLE", "versionCode": ` + strconv.FormatInt(versionCode, 10) + `}]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	return path
}

func baseOptions(aab string) app.PublishOptions {
	return app.PublishOptions{
		Package: "com.example.app",
		AABPath: aab,
		Track:   "internal",
		Status:  "completed",
	}
}

func TestPublishSuccess(t *testing.T) {
	fake := &playtest.Fake{}
	publisher := app.NewPublisher(fake, nil)

	result, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t)))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if result.VersionCode != 42 || result.Review != app.ReviewSent {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.EditID != "edit-1" {
		t.Errorf("EditID = %q", result.EditID)
	}

	want := []string{"TrackReleases", "CreateEdit", "UploadBundle", "UpdateTrack", "CommitEdit"}
	if strings.Join(fake.Calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", fake.Calls, want)
	}
	if len(fake.DeletedEditIDs) != 0 {
		t.Fatalf("edit must not be deleted after commit: %v", fake.DeletedEditIDs)
	}
}

func TestPublishReleaseNotes(t *testing.T) {
	fake := &playtest.Fake{}
	publisher := app.NewPublisher(fake, nil)

	opts := baseOptions(writeAAB(t))
	opts.Notes = map[string]string{"en-US": "Bug fixes"}
	if _, err := publisher.Publish(context.Background(), opts); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(fake.UpdatedReleases) != 1 {
		t.Fatalf("updated releases = %d, want 1", len(fake.UpdatedReleases))
	}
	notes := fake.UpdatedReleases[0].ReleaseNotes
	if len(notes) != 1 || notes[0].Language != "en-US" || notes[0].Text != "Bug fixes" {
		t.Fatalf("unexpected notes: %#v", notes)
	}
}

func TestPublishReleaseName(t *testing.T) {
	fake := &playtest.Fake{}
	publisher := app.NewPublisher(fake, nil)

	opts := baseOptions(writeAAB(t))
	opts.ReleaseName = "1.0.35"
	if _, err := publisher.Publish(context.Background(), opts); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(fake.UpdatedReleases) != 1 {
		t.Fatalf("updated releases = %d, want 1", len(fake.UpdatedReleases))
	}
	if got := fake.UpdatedReleases[0].Name; got != "1.0.35" {
		t.Fatalf("release name = %q, want %q", got, "1.0.35")
	}
}

func TestPublishWithoutReleaseName(t *testing.T) {
	fake := &playtest.Fake{}
	publisher := app.NewPublisher(fake, nil)

	if _, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t))); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(fake.UpdatedReleases) != 1 {
		t.Fatalf("updated releases = %d, want 1", len(fake.UpdatedReleases))
	}
	if got := fake.UpdatedReleases[0].Name; got != "" {
		t.Fatalf("release name = %q, want empty", got)
	}
}

func TestPublishSkipExistingVersion(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return []play.ReleaseSummary{{VersionCodes: []int64{42}}}, nil
		},
	}
	publisher := app.NewPublisher(fake, nil)

	opts := baseOptions(writeAAB(t))
	opts.MetadataPath = writeMetadata(t, 42)
	opts.IfExists = "skip"

	result, err := publisher.Publish(context.Background(), opts)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result.Review != app.ReviewSkipped || result.VersionCode != 42 {
		t.Fatalf("unexpected result: %#v", result)
	}
	for _, call := range fake.Calls {
		if call == "CreateEdit" {
			t.Fatal("edit must not be created when the version is skipped")
		}
	}
}

func TestPublishFailExistingVersion(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return []play.ReleaseSummary{{VersionCodes: []int64{42}}}, nil
		},
	}
	publisher := app.NewPublisher(fake, nil)

	opts := baseOptions(writeAAB(t))
	opts.MetadataPath = writeMetadata(t, 42)

	_, err := publisher.Publish(context.Background(), opts)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Conflict {
		t.Fatalf("code = %d, want %d", got, exitcode.Conflict)
	}
	if len(fake.DeletedEditIDs) != 0 {
		t.Fatalf("no edit must be created: %v", fake.DeletedEditIDs)
	}
}

func TestPublishSkipWithVersionCode(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return []play.ReleaseSummary{{VersionCodes: []int64{42}}}, nil
		},
	}
	publisher := app.NewPublisher(fake, nil)

	opts := baseOptions(writeAAB(t))
	opts.VersionCode = 42
	opts.IfExists = "skip"

	result, err := publisher.Publish(context.Background(), opts)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result.Review != app.ReviewSkipped || result.VersionCode != 42 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if contains(fake.Calls, "CreateEdit") {
		t.Fatal("edit must not be created when the version is skipped")
	}
}

func TestPublishFailExistingVersionWithVersionCode(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return []play.ReleaseSummary{{VersionCodes: []int64{42}}}, nil
		},
	}
	publisher := app.NewPublisher(fake, nil)

	opts := baseOptions(writeAAB(t))
	opts.VersionCode = 42

	_, err := publisher.Publish(context.Background(), opts)
	if got := exitcode.From(err); got != exitcode.Conflict {
		t.Fatalf("code = %d, want %d", got, exitcode.Conflict)
	}
}

func TestPublishDryRun(t *testing.T) {
	fake := &playtest.Fake{}
	publisher := app.NewPublisher(fake, nil)

	opts := baseOptions(writeAAB(t))
	opts.DryRun = true

	result, err := publisher.Publish(context.Background(), opts)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result.Review != app.ReviewValidated {
		t.Fatalf("review = %q, want %q", result.Review, app.ReviewValidated)
	}

	if !contains(fake.Calls, "ValidateEdit") || !contains(fake.Calls, "DeleteEdit") {
		t.Fatalf("dry run must validate and delete the edit: %v", fake.Calls)
	}
	if contains(fake.Calls, "CommitEdit") {
		t.Fatal("dry run must not commit")
	}
}

func TestPublishDryRunCleanupFailure(t *testing.T) {
	cleanupErr := exitcode.Errorf(exitcode.Transient, "service unavailable")
	fake := &playtest.Fake{DeleteEditFunc: func(ctx context.Context, _, _ string) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("cleanup must have a bounded context")
		}
		return cleanupErr
	}}
	opts := baseOptions(writeAAB(t))
	opts.DryRun = true
	result, err := app.NewPublisher(fake, nil).Publish(context.Background(), opts)
	if exitcode.From(err) != exitcode.Transient || !errors.Is(err, cleanupErr) {
		t.Fatalf("cleanup failure was not preserved: %v", err)
	}
	if !strings.Contains(err.Error(), "edit-1") || result.Review != "" {
		t.Fatalf("misleading dry-run result: %#v, %v", result, err)
	}
	if len(fake.DeletedEditIDs) != 1 || contains(fake.Calls, "CommitEdit") {
		t.Fatalf("unexpected API calls: %v", fake.Calls)
	}
}

func TestPublishCleanupDoesNotMaskValidationError(t *testing.T) {
	validationErr := exitcode.Errorf(exitcode.Rejected, "invalid release")
	fake := &playtest.Fake{
		ValidateEditFunc: func(context.Context, string, string) error { return validationErr },
		DeleteEditFunc: func(context.Context, string, string) error {
			return exitcode.Errorf(exitcode.Transient, "cleanup failed")
		},
	}
	opts := baseOptions(writeAAB(t))
	opts.DryRun = true
	_, err := app.NewPublisher(fake, nil).Publish(context.Background(), opts)
	if !errors.Is(err, validationErr) || exitcode.From(err) != exitcode.Rejected {
		t.Fatalf("cleanup masked the original failure: %v", err)
	}
}

func TestPublishRetriesManualReview(t *testing.T) {
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
	publisher := app.NewPublisher(fake, nil)

	result, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t)))
	if err == nil {
		t.Fatal("expected the manual-action error")
	}
	if got := exitcode.From(err); got != exitcode.ManualAction {
		t.Fatalf("code = %d, want %d", got, exitcode.ManualAction)
	}
	if result.Review != app.ReviewNotSent {
		t.Fatalf("review = %q, want %q", result.Review, app.ReviewNotSent)
	}
	if result.VersionCode != 42 || result.EditID != "edit-1" {
		t.Fatalf("saved release is not reported: %#v", result)
	}
	if len(fake.DeletedEditIDs) != 0 {
		t.Fatalf("committed edit must not be deleted: %v", fake.DeletedEditIDs)
	}
	if len(fake.CommitOptions) != 2 {
		t.Fatalf("commit attempts = %d, want 2", len(fake.CommitOptions))
	}
	if !fake.CommitOptions[1].ChangesNotSentForReview {
		t.Fatal("retry must set changesNotSentForReview")
	}
}

func TestPublishManualReviewAfterTransientRetry(t *testing.T) {
	attempts := 0
	fake := &playtest.Fake{
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			attempts++
			switch attempts {
			case 1:
				return exitcode.Errorf(exitcode.ManualAction, "changes cannot be sent for review automatically")
			case 2:
				return ambiguousTimeout()
			default:
				return nil
			}
		},
	}
	publisher := app.NewPublisher(fake, nil)

	result, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t)))
	if got := exitcode.From(err); got != exitcode.ManualAction {
		t.Fatalf("code = %d, want %d (err: %v)", got, exitcode.ManualAction, err)
	}
	if result.Review != app.ReviewNotSent {
		t.Fatalf("review = %q, want %q: the successful commit saved without review", result.Review, app.ReviewNotSent)
	}
	if len(fake.CommitOptions) != 3 {
		t.Fatalf("commit attempts = %d, want 3", len(fake.CommitOptions))
	}
	if !fake.CommitOptions[2].ChangesNotSentForReview {
		t.Fatal("the third attempt must keep changesNotSentForReview")
	}
}

func TestPublishManualReviewVerifiedWithoutReview(t *testing.T) {
	attempts := 0
	releases := 0
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			releases++
			if releases == 1 {
				return nil, nil // preflight
			}
			return []play.ReleaseSummary{{VersionCodes: []int64{42}}}, nil
		},
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			attempts++
			if attempts == 1 {
				return exitcode.Errorf(exitcode.ManualAction, "changes cannot be sent for review automatically")
			}
			return ambiguousTimeout()
		},
	}
	publisher := app.NewPublisher(fake, nil)

	result, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t)))
	if got := exitcode.From(err); got != exitcode.ManualAction {
		t.Fatalf("code = %d, want %d (err: %v)", got, exitcode.ManualAction, err)
	}
	if result.Review != app.ReviewNotSent {
		t.Fatalf("review = %q, want %q", result.Review, app.ReviewNotSent)
	}
	if len(fake.CommitOptions) != 2 {
		t.Fatalf("commit attempts = %d, want 2", len(fake.CommitOptions))
	}
}

func TestPublishVerifiesFinalTransientRetry(t *testing.T) {
	releases := 0
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			releases++
			if releases <= 2 {
				return nil, nil // preflight and the first reconciliation
			}
			return []play.ReleaseSummary{{VersionCodes: []int64{42}, State: play.LifecycleInReview}}, nil
		},
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			return ambiguousTimeout()
		},
	}
	publisher := app.NewPublisher(fake, nil)

	result, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t)))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result.Review != app.ReviewSent || result.EditID != "edit-1" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(fake.CommitOptions) != 2 {
		t.Fatalf("commit attempts = %d, want 2", len(fake.CommitOptions))
	}
	if len(fake.DeletedEditIDs) != 0 {
		t.Fatalf("reconciled release must not delete the edit: %v", fake.DeletedEditIDs)
	}
}

func TestPublishUnknownCommitState(t *testing.T) {
	releases := 0
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			releases++
			if releases == 1 {
				return nil, nil
			}
			return nil, exitcode.Errorf(exitcode.Transient, "network down")
		},
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			return ambiguousTimeout()
		},
	}
	publisher := app.NewPublisher(fake, nil)

	_, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t)))
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Transient {
		t.Fatalf("code = %d, want %d", got, exitcode.Transient)
	}

	var coded *exitcode.Error
	if !errors.As(err, &coded) || !strings.Contains(coded.Hint, "check the track") {
		t.Fatalf("expected the unknown-state hint, got %v", err)
	}
	if len(fake.CommitOptions) != 1 {
		t.Fatalf("commit attempts = %d, want 1", len(fake.CommitOptions))
	}
	if len(fake.DeletedEditIDs) != 0 {
		t.Fatalf("an unknown commit must keep the edit: %v", fake.DeletedEditIDs)
	}
}

func TestPublishVerifiesAmbiguousCommit(t *testing.T) {
	preflight := true
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			if preflight {
				preflight = false
				return nil, nil
			}
			return []play.ReleaseSummary{{VersionCodes: []int64{42}, State: play.LifecycleInReview}}, nil
		},
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			return ambiguousTimeout()
		},
	}
	publisher := app.NewPublisher(fake, nil)

	result, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t)))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result.VersionCode != 42 || result.Review != app.ReviewSent {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(fake.CommitOptions) != 1 {
		t.Fatalf("commit attempts = %d, want 1", len(fake.CommitOptions))
	}
}

func TestPublishRetriesTransientCommit(t *testing.T) {
	fake := &playtest.Fake{
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			return ambiguousTimeout()
		},
	}
	publisher := app.NewPublisher(fake, nil)

	_, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t)))
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Transient {
		t.Fatalf("code = %d, want %d", got, exitcode.Transient)
	}
	if len(fake.CommitOptions) != 2 {
		t.Fatalf("commit attempts = %d, want 2", len(fake.CommitOptions))
	}
	if len(fake.DeletedEditIDs) != 1 {
		t.Fatalf("edit must be deleted: %v", fake.DeletedEditIDs)
	}
}

func TestPublishUploadErrorDeletesEdit(t *testing.T) {
	fake := &playtest.Fake{
		UploadBundleFunc: func(context.Context, string, string, io.Reader, int64) (play.BundleInfo, error) {
			return play.BundleInfo{}, exitcode.Errorf(exitcode.Rejected, "bad bundle")
		},
	}
	publisher := app.NewPublisher(fake, nil)

	_, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t)))
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(fake.DeletedEditIDs) != 1 || fake.DeletedEditIDs[0] != "edit-1" {
		t.Fatalf("edit must be deleted: %v", fake.DeletedEditIDs)
	}
}

func TestPublishMetadataVersionMismatch(t *testing.T) {
	publisher := app.NewPublisher(&playtest.Fake{}, nil)

	opts := baseOptions(writeAAB(t))
	opts.MetadataPath = writeMetadata(t, 41)

	_, err := publisher.Publish(context.Background(), opts)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Usage {
		t.Fatalf("code = %d, want %d", got, exitcode.Usage)
	}
}

func TestPrepareRejectsConflictingVersionCodes(t *testing.T) {
	aab := writeAABWithIdentity(t, "com.example.app", 41, "1.0.41")

	tests := []struct {
		name        string
		metaCode    int64
		versionCode int64
		wantCode    exitcode.Code
	}{
		{"conflict", 41, 42, exitcode.Usage},
		{"matching", 41, 41, exitcode.OK},
		{"only metadata", 41, 0, exitcode.OK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := baseOptions(aab)
			opts.MetadataPath = writeMetadata(t, test.metaCode)
			opts.VersionCode = test.versionCode

			prepared, err := app.Prepare(opts)
			defer func() { _ = prepared.Close() }()
			if got := exitcode.From(err); got != test.wantCode {
				t.Fatalf("code = %d, want %d (err: %v)", got, test.wantCode, err)
			}
			if test.wantCode == exitcode.Usage &&
				!strings.Contains(err.Error(), "metadata versionCode 41 does not match --version-code 42") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPrepareValidatesPolicyOptions(t *testing.T) {
	aab := writeAAB(t)

	tests := []struct {
		name   string
		mutate func(*app.PublishOptions)
	}{
		{"invalid if-exists", func(o *app.PublishOptions) { o.IfExists = "sometimes" }},
		{"invalid in-review", func(o *app.PublishOptions) { o.InReviewBehavior = "ignore" }},
		{"dry-run with save", func(o *app.PublishOptions) {
			o.DryRun = true
			o.ChangesNotSentForReview = true
		}},
		{"negative version code", func(o *app.PublishOptions) { o.VersionCode = -1 }},
		{"missing aab", func(o *app.PublishOptions) { o.AABPath = "" }},
		{"package mismatch", func(o *app.PublishOptions) { o.Package = "com.example.other" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := baseOptions(aab)
			test.mutate(&opts)

			if _, err := app.Prepare(opts); exitcode.From(err) != exitcode.Usage {
				t.Fatalf("code = %d, want %d (err: %v)", exitcode.From(err), exitcode.Usage, err)
			}
		})
	}
}

func TestPrepareResolvesPackageFromMetadata(t *testing.T) {
	opts := baseOptions(writeAAB(t))
	opts.Package = ""
	opts.MetadataPath = writeMetadata(t, 42)

	prepared, err := app.Prepare(opts)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	result, err := app.NewPublisher(&playtest.Fake{}, nil).
		PublishPrepared(context.Background(), prepared)
	if err != nil {
		t.Fatalf("PublishPrepared: %v", err)
	}
	if result.Package != "com.example.app" {
		t.Fatalf("package = %q, want com.example.app", result.Package)
	}
}

func TestPublishPolicyValidationBeforeAPI(t *testing.T) {
	fake := &playtest.Fake{}
	publisher := app.NewPublisher(fake, nil)

	opts := baseOptions(writeAAB(t))
	opts.InReviewBehavior = "ignore"

	_, err := publisher.Publish(context.Background(), opts)
	if got := exitcode.From(err); got != exitcode.Usage {
		t.Fatalf("code = %d, want %d (err: %v)", got, exitcode.Usage, err)
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("API was called for invalid input: %v", fake.Calls)
	}
}

func TestPublishPreparedRejectsZeroValue(t *testing.T) {
	publisher := app.NewPublisher(&playtest.Fake{}, nil)

	_, err := publisher.PublishPrepared(context.Background(), app.Prepared{})
	if got := exitcode.From(err); got != exitcode.Internal {
		t.Fatalf("code = %d, want %d (err: %v)", got, exitcode.Internal, err)
	}
}

func TestPublishUploadedHashMismatch(t *testing.T) {
	fake := &playtest.Fake{
		UploadBundleFunc: func(context.Context, string, string, io.Reader, int64) (play.BundleInfo, error) {
			return play.BundleInfo{VersionCode: 42, SHA256: strings.Repeat("0", 64)}, nil
		},
	}
	publisher := app.NewPublisher(fake, nil)

	_, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t)))
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Internal {
		t.Fatalf("code = %d, want %d", got, exitcode.Internal)
	}
	if contains(fake.Calls, "UpdateTrack") {
		t.Fatal("a hash mismatch must stop before tracks.update")
	}
	if len(fake.DeletedEditIDs) != 1 {
		t.Fatalf("edit must be deleted: %v", fake.DeletedEditIDs)
	}
}

func TestPublishUploadedHashMatches(t *testing.T) {
	aab := writeAAB(t)
	info, err := artifact.Inspect(aab)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}

	fake := &playtest.Fake{
		UploadBundleFunc: func(context.Context, string, string, io.Reader, int64) (play.BundleInfo, error) {
			return play.BundleInfo{VersionCode: 42, SHA256: strings.ToUpper(info.SHA256)}, nil
		},
	}
	publisher := app.NewPublisher(fake, nil)

	result, err := publisher.Publish(context.Background(), baseOptions(aab))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result.SHA256 != strings.ToUpper(info.SHA256) {
		t.Fatalf("result sha256 = %q, want the uploaded hash", result.SHA256)
	}
	if !contains(fake.Calls, "UpdateTrack") {
		t.Fatalf("matching hashes must continue: %v", fake.Calls)
	}
}

func TestPublishInvalidPlanDoesNotCallAPI(t *testing.T) {
	fake := &playtest.Fake{}
	publisher := app.NewPublisher(fake, nil)

	opts := baseOptions(writeAAB(t))
	fraction := 1.0
	opts.Rollout = &fraction

	_, err := publisher.Publish(context.Background(), opts)
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("API must not be called: %v", fake.Calls)
	}
}

func TestPublishOptionalRollout(t *testing.T) {
	fraction := 0.25
	zero := 0.0
	for _, test := range []struct {
		name         string
		rollout      *float64
		status       string
		wantStatus   string
		wantFraction float64
		wantCode     exitcode.Code
	}{
		{name: "omitted", wantStatus: "completed"},
		{name: "staged", rollout: &fraction, wantStatus: "inProgress", wantFraction: fraction},
		{name: "explicit zero", rollout: &zero, wantCode: exitcode.Usage},
		{name: "completed with fraction", rollout: &fraction, status: "completed", wantCode: exitcode.Usage},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &playtest.Fake{}
			opts := baseOptions(writeAAB(t))
			opts.Rollout, opts.Status = test.rollout, test.status
			_, err := app.NewPublisher(fake, nil).Publish(context.Background(), opts)
			if exitcode.From(err) != test.wantCode {
				t.Fatalf("unexpected error: %v", err)
			}
			if test.wantCode != exitcode.OK {
				if len(fake.Calls) != 0 {
					t.Fatalf("invalid rollout reached API: %v", fake.Calls)
				}
				return
			}
			if len(fake.UpdatedReleases) != 1 {
				t.Fatalf("unexpected releases: %v", fake.UpdatedReleases)
			}
			rel := fake.UpdatedReleases[0]
			if rel.Status != test.wantStatus || rel.UserFraction != test.wantFraction {
				t.Fatalf("unexpected release: %#v", rel)
			}
		})
	}
}

func TestPublishMissingAAB(t *testing.T) {
	publisher := app.NewPublisher(&playtest.Fake{}, nil)

	_, err := publisher.Publish(context.Background(), baseOptions(filepath.Join(t.TempDir(), "missing.aab")))
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Usage {
		t.Fatalf("code = %d, want %d", got, exitcode.Usage)
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

func TestPrepareResolvesPackageAndVersionFromAAB(t *testing.T) {
	opts := baseOptions(writeAAB(t))
	opts.Package = ""
	opts.VersionCode = 0
	opts.MetadataPath = ""

	prepared, err := app.Prepare(opts)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer func() { _ = prepared.Close() }()

	if prepared.Package() != "com.example.app" {
		t.Fatalf("package = %q, want com.example.app", prepared.Package())
	}
}

func TestPrepareRejectsAABVersionMismatch(t *testing.T) {
	opts := baseOptions(writeAAB(t))
	opts.VersionCode = 41

	prepared, err := app.Prepare(opts)
	defer func() { _ = prepared.Close() }()
	if exitcode.From(err) != exitcode.Usage {
		t.Fatalf("code = %d, want %d (err: %v)", exitcode.From(err), exitcode.Usage, err)
	}
	if !strings.Contains(err.Error(), "does not match AAB versionCode 42") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareRejectsMetadataAABVersionMismatch(t *testing.T) {
	opts := baseOptions(writeAAB(t))
	opts.MetadataPath = writeMetadata(t, 41)

	prepared, err := app.Prepare(opts)
	defer func() { _ = prepared.Close() }()
	if exitcode.From(err) != exitcode.Usage {
		t.Fatalf("code = %d, want %d (err: %v)", exitcode.From(err), exitcode.Usage, err)
	}
	if !strings.Contains(err.Error(), "metadata versionCode 41 does not match AAB versionCode 42") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublishSkipWithoutMetadata(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return []play.ReleaseSummary{{VersionCodes: []int64{42}}}, nil
		},
	}
	publisher := app.NewPublisher(fake, nil)

	opts := baseOptions(writeAAB(t))
	opts.IfExists = "skip"

	result, err := publisher.Publish(context.Background(), opts)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result.Review != app.ReviewSkipped || result.VersionCode != 42 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.VersionName != "1.0.42" {
		t.Fatalf("version name = %q, want 1.0.42", result.VersionName)
	}
	if contains(fake.Calls, "UploadBundle") {
		t.Fatalf("skip must not upload: %v", fake.Calls)
	}
}

func TestPublishUploadsTheInspectedFileHandle(t *testing.T) {
	path := writeAAB(t)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original AAB: %v", err)
	}

	prepared, err := app.Prepare(baseOptions(path))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer func() { _ = prepared.Close() }()

	replacement := writeAABWithIdentity(t, "com.example.replacement", 43, "2.0")
	replacementBytes, err := os.ReadFile(replacement)
	if err != nil {
		t.Fatalf("read replacement AAB: %v", err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("replace AAB path: %v", err)
	}

	var uploaded []byte
	fake := &playtest.Fake{
		UploadBundleFunc: func(_ context.Context, _, _ string, r io.Reader, _ int64) (play.BundleInfo, error) {
			uploaded, err = io.ReadAll(r)
			if err != nil {
				return play.BundleInfo{}, err
			}
			sum := sha256.Sum256(uploaded)
			return play.BundleInfo{VersionCode: 42, SHA256: hex.EncodeToString(sum[:])}, nil
		},
	}

	result, err := app.NewPublisher(fake, nil).PublishPrepared(context.Background(), prepared)
	if err != nil {
		t.Fatalf("PublishPrepared: %v", err)
	}
	if !bytes.Equal(uploaded, original) {
		t.Fatal("upload did not use the bytes inspected by Prepare")
	}
	if bytes.Equal(uploaded, replacementBytes) {
		t.Fatal("upload used the replacement file")
	}
	if result.VersionName != "1.0.42" {
		t.Fatalf("version name = %q, want 1.0.42", result.VersionName)
	}
}

func TestPublishDefinitiveCommitRejectionIsNotReconciled(t *testing.T) {
	trackReads := 0
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			trackReads++
			if trackReads == 1 {
				return nil, nil // preflight
			}
			// A version that appeared through another operation must not turn
			// the rejected commit into success.
			return []play.ReleaseSummary{{VersionCodes: []int64{42}, State: play.LifecyclePublished}}, nil
		},
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			return exitcode.Errorf(exitcode.Conflict, "conflict")
		},
	}
	publisher := app.NewPublisher(fake, nil)

	result, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t)))
	if err == nil {
		t.Fatal("expected the conflict")
	}
	if got := exitcode.From(err); got != exitcode.Conflict {
		t.Fatalf("code = %d, want %d (err: %v)", got, exitcode.Conflict, err)
	}
	if result.VersionCode != 0 || result.Review != "" {
		t.Fatalf("a rejected commit must not report a release: %#v", result)
	}
	if trackReads != 1 {
		t.Fatalf("a definitive rejection must not be reconciled, track reads = %d", trackReads)
	}
	if len(fake.DeletedEditIDs) != 1 {
		t.Fatalf("a rejected edit must be deleted: %v", fake.DeletedEditIDs)
	}
}

func TestPublishAmbiguousCommitObservedNotSent(t *testing.T) {
	preflight := true
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			if preflight {
				preflight = false
				return nil, nil
			}
			return []play.ReleaseSummary{{
				VersionCodes: []int64{42},
				State:        play.LifecycleNotSentForReview,
			}}, nil
		},
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			return ambiguousTimeout()
		},
	}
	publisher := app.NewPublisher(fake, nil)

	result, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t)))
	if got := exitcode.From(err); got != exitcode.ManualAction {
		t.Fatalf("code = %d, want %d (err: %v)", got, exitcode.ManualAction, err)
	}
	if result.Review != app.ReviewNotSent || result.VersionCode != 42 {
		t.Fatalf("unexpected result: %#v", result)
	}
	var coded *exitcode.Error
	if !errors.As(err, &coded) || !strings.Contains(coded.Hint, "Play Console") {
		t.Fatalf("expected the manual-action hint, got %v", err)
	}
	if len(fake.DeletedEditIDs) != 0 {
		t.Fatalf("a saved release must keep the edit: %v", fake.DeletedEditIDs)
	}
}

func TestPublishAmbiguousCommitObservedDraft(t *testing.T) {
	preflight := true
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			if preflight {
				preflight = false
				return nil, nil
			}
			return []play.ReleaseSummary{{
				VersionCodes: []int64{42},
				State:        play.LifecycleNotSentForReview,
			}}, nil
		},
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			return ambiguousTimeout()
		},
	}
	publisher := app.NewPublisher(fake, nil)

	opts := baseOptions(writeAAB(t))
	opts.Status = "draft"
	result, err := publisher.Publish(context.Background(), opts)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result.Review != app.ReviewNotSent {
		t.Fatalf("review = %q, want %q", result.Review, app.ReviewNotSent)
	}
	if len(fake.DeletedEditIDs) != 0 {
		t.Fatalf("a saved draft must keep the edit: %v", fake.DeletedEditIDs)
	}
}

func TestPublishExplicitSaveNotSentStaysSuccess(t *testing.T) {
	preflight := true
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			if preflight {
				preflight = false
				return nil, nil
			}
			return []play.ReleaseSummary{{
				VersionCodes: []int64{42},
				State:        play.LifecycleNotSentForReview,
			}}, nil
		},
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			return ambiguousTimeout()
		},
	}
	publisher := app.NewPublisher(fake, nil)

	opts := baseOptions(writeAAB(t))
	opts.ChangesNotSentForReview = true
	result, err := publisher.Publish(context.Background(), opts)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result.Review != app.ReviewNotSent {
		t.Fatalf("review = %q, want %q", result.Review, app.ReviewNotSent)
	}
}

func TestPublishCommitRecoversAfterDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	trackReads := 0
	recovered := false
	fake := &playtest.Fake{
		TrackReleasesFunc: func(readCtx context.Context, _, _ string) ([]play.ReleaseSummary, error) {
			trackReads++
			if trackReads == 1 {
				return nil, nil // preflight
			}
			if readCtx.Err() != nil {
				return nil, readCtx.Err()
			}
			recovered = true
			return []play.ReleaseSummary{{
				VersionCodes: []int64{42},
				State:        play.LifecycleInReview,
			}}, nil
		},
		CommitEditFunc: func(commitCtx context.Context, _, _ string, _ play.CommitOptions) error {
			<-commitCtx.Done()
			return ambiguousTimeout()
		},
	}
	publisher := app.NewPublisher(fake, nil)

	result, err := publisher.Publish(ctx, baseOptions(writeAAB(t)))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if !recovered {
		t.Fatal("expected reconciliation under the recovery budget")
	}
	if result.Review != app.ReviewSent || result.VersionCode != 42 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(fake.DeletedEditIDs) != 0 {
		t.Fatalf("a recovered release must keep the edit: %v", fake.DeletedEditIDs)
	}
}

func TestPublishCancelledCommitKeepsEdit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	trackReads := 0
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			trackReads++
			return nil, nil
		},
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			cancel()
			return exitcode.Wrap(exitcode.Interrupted, &play.AmbiguousFailure{Err: context.Canceled})
		},
	}
	publisher := app.NewPublisher(fake, nil)

	_, err := publisher.Publish(ctx, baseOptions(writeAAB(t)))
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Interrupted {
		t.Fatalf("code = %d, want %d", got, exitcode.Interrupted)
	}
	if trackReads != 1 {
		t.Fatalf("a canceled commit must not be reconciled, track reads = %d", trackReads)
	}
	if len(fake.DeletedEditIDs) != 0 {
		t.Fatalf("a canceled commit must keep the edit: %v", fake.DeletedEditIDs)
	}
}

func TestPublishCancelledUploadKeepsEdit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fake := &playtest.Fake{
		UploadBundleFunc: func(context.Context, string, string, io.Reader, int64) (play.BundleInfo, error) {
			cancel()
			return play.BundleInfo{}, exitcode.Wrap(exitcode.Interrupted, context.Canceled)
		},
	}
	publisher := app.NewPublisher(fake, nil)

	_, err := publisher.Publish(ctx, baseOptions(writeAAB(t)))
	if got := exitcode.From(err); got != exitcode.Interrupted {
		t.Fatalf("code = %d, want %d (%v)", got, exitcode.Interrupted, err)
	}
	if len(fake.DeletedEditIDs) != 0 {
		t.Fatalf("a signal must not send a delete request: %v", fake.DeletedEditIDs)
	}
	if contains(fake.Calls, "CommitEdit") {
		t.Fatalf("a canceled upload must not commit: %v", fake.Calls)
	}
}

func TestPublishCancelledDryRunKeepsEdit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fake := &playtest.Fake{
		ValidateEditFunc: func(context.Context, string, string) error {
			cancel()
			return nil
		},
	}
	opts := baseOptions(writeAAB(t))
	opts.DryRun = true

	result, err := app.NewPublisher(fake, nil).Publish(ctx, opts)
	if got := exitcode.From(err); got != exitcode.Interrupted {
		t.Fatalf("code = %d, want %d (%v)", got, exitcode.Interrupted, err)
	}
	if len(fake.DeletedEditIDs) != 0 {
		t.Fatalf("a signal must not send a delete request: %v", fake.DeletedEditIDs)
	}
	if result.Review != "" {
		t.Fatalf("a canceled dry run must not report validation: %#v", result)
	}
}

func TestPublishSignalDuringReconciliationKeepsUnknownState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	trackReads := 0
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			trackReads++
			if trackReads == 1 {
				return nil, nil // preflight
			}
			// The signal arrives while the reconciliation read is running, so
			// the reported failure is not itself a cancellation.
			cancel()
			return nil, exitcode.Errorf(exitcode.Transient, "service unavailable")
		},
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			return ambiguousTimeout()
		},
	}
	publisher := app.NewPublisher(fake, nil)

	_, err := publisher.Publish(ctx, baseOptions(writeAAB(t)))
	if got := exitcode.From(err); got != exitcode.Interrupted {
		t.Fatalf("code = %d, want %d (%v)", got, exitcode.Interrupted, err)
	}
	if !app.IsCommitStateUnknown(err) {
		t.Fatalf("the unknown commit state must stay recognizable: %v", err)
	}
	if !strings.Contains(err.Error(), "commit state is unknown") || !strings.Contains(err.Error(), "edit-1") {
		t.Fatalf("missing unknown-state diagnostic: %v", err)
	}
	if trackReads != 2 {
		t.Fatalf("track reads = %d, want 2 (preflight and reconciliation)", trackReads)
	}
	if len(fake.DeletedEditIDs) != 0 {
		t.Fatalf("an interrupted reconciliation must keep the edit: %v", fake.DeletedEditIDs)
	}
}

// ambiguousTimeout builds the error that the Play adapter produces for a
// transport failure whose commit may have been applied by the server.
func ambiguousTimeout() error {
	return exitcode.Wrap(exitcode.Transient,
		&play.AmbiguousFailure{Err: errors.New("timeout")})
}

func TestPublishUntypedTransientCommitIsNotReconciled(t *testing.T) {
	trackReads := 0
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			trackReads++
			return nil, nil
		},
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			// Not marked as ambiguous by the adapter: it cannot hide an
			// applied edit, so reconciliation must not run.
			return exitcode.Errorf(exitcode.Transient, "injected")
		},
	}
	publisher := app.NewPublisher(fake, nil)

	_, err := publisher.Publish(context.Background(), baseOptions(writeAAB(t)))
	if got := exitcode.From(err); got != exitcode.Transient {
		t.Fatalf("code = %d, want %d", got, exitcode.Transient)
	}
	if trackReads != 1 {
		t.Fatalf("an untyped transient failure must not be reconciled, track reads = %d", trackReads)
	}
	if len(fake.DeletedEditIDs) != 1 {
		t.Fatalf("a failed commit must delete the edit: %v", fake.DeletedEditIDs)
	}
}

func TestPublishSignalInterruptsRecovery(t *testing.T) {
	opCtx, opCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer opCancel()
	signalCtx, signal := context.WithCancel(context.Background())
	defer signal()

	trackReads := 0
	fake := &playtest.Fake{
		TrackReleasesFunc: func(readCtx context.Context, _, _ string) ([]play.ReleaseSummary, error) {
			trackReads++
			switch trackReads {
			case 1:
				return nil, nil // preflight
			case 2:
				return nil, readCtx.Err() // the expired operation context
			default:
				signal()
				<-readCtx.Done()
				if !errors.Is(readCtx.Err(), context.Canceled) {
					t.Errorf("recovery was not interrupted by the signal: %v", readCtx.Err())
				}
				return nil, readCtx.Err()
			}
		},
		CommitEditFunc: func(commitCtx context.Context, _, _ string, _ play.CommitOptions) error {
			<-commitCtx.Done()
			return ambiguousTimeout()
		},
	}
	publisher := app.NewPublisher(fake, nil, app.WithRecoveryBase(signalCtx))

	_, err := publisher.Publish(opCtx, baseOptions(writeAAB(t)))
	if err == nil {
		t.Fatal("expected an unknown-state error")
	}
	if got := exitcode.From(err); got != exitcode.Interrupted {
		t.Fatalf("code = %d, want %d", got, exitcode.Interrupted)
	}
	if trackReads != 3 {
		t.Fatalf("track reads = %d, want 3 (preflight, expired read, recovery)", trackReads)
	}
	if len(fake.DeletedEditIDs) != 0 {
		t.Fatalf("an interrupted recovery must keep the edit: %v", fake.DeletedEditIDs)
	}
}

func TestPreparedRejectsReuseAfterClose(t *testing.T) {
	fake := &playtest.Fake{}
	publisher := app.NewPublisher(fake, nil)

	prepared, err := app.Prepare(baseOptions(writeAAB(t)))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	_, err = publisher.PublishPrepared(context.Background(), prepared)
	if got := exitcode.From(err); got != exitcode.Internal {
		t.Fatalf("code = %d, want %d (err: %v)", got, exitcode.Internal, err)
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("a closed Prepared must fail before any API call: %v", fake.Calls)
	}
}

func TestPreparedRejectsSecondPublish(t *testing.T) {
	fake := &playtest.Fake{}
	publisher := app.NewPublisher(fake, nil)

	prepared, err := app.Prepare(baseOptions(writeAAB(t)))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := publisher.PublishPrepared(context.Background(), prepared); err != nil {
		t.Fatalf("first PublishPrepared: %v", err)
	}
	if _, err := publisher.PublishPrepared(context.Background(), prepared); err == nil {
		t.Fatal("expected the second PublishPrepared to fail")
	} else if got := exitcode.From(err); got != exitcode.Internal {
		t.Fatalf("code = %d, want %d", got, exitcode.Internal)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("Close after publishing: %v", err)
	}

	edits := 0
	for _, call := range fake.Calls {
		if call == "CreateEdit" {
			edits++
		}
	}
	if edits != 1 {
		t.Fatalf("edits created = %d, want 1", edits)
	}
}
