package app_test

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/artifact"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
)

func writeAAB(t *testing.T) string {
	return writeBundleFiles(t, map[string]string{"BundleConfig.pb": "bundle-config"})
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
				return exitcode.Errorf(exitcode.Transient, "timeout")
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
			return exitcode.Errorf(exitcode.Transient, "timeout")
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
			return []play.ReleaseSummary{{VersionCodes: []int64{42}}}, nil
		},
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			return exitcode.Errorf(exitcode.Transient, "timeout")
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
			return exitcode.Errorf(exitcode.Transient, "timeout")
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
	if !errors.As(err, &coded) || !strings.Contains(coded.Hint, "state is unknown") {
		t.Fatalf("expected the unknown-state hint, got %v", err)
	}
	if len(fake.CommitOptions) != 1 {
		t.Fatalf("commit attempts = %d, want 1", len(fake.CommitOptions))
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
			return []play.ReleaseSummary{{VersionCodes: []int64{42}}}, nil
		},
		CommitEditFunc: func(context.Context, string, string, play.CommitOptions) error {
			return exitcode.Errorf(exitcode.Transient, "timeout")
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
			return exitcode.Errorf(exitcode.Transient, "timeout")
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
	aab := writeAAB(t)

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

			_, err := app.Prepare(opts)
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
		{"missing package", func(o *app.PublishOptions) { o.Package = "" }},
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
	opts.Rollout = 1
	opts.RolloutSet = true

	_, err := publisher.Publish(context.Background(), opts)
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("API must not be called: %v", fake.Calls)
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
