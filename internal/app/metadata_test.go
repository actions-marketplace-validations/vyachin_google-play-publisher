package app_test

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
)

func writeMetadataFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeMetadataImage(t *testing.T, path string, width, height int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()

	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}

// writeListingDir creates a metadata directory with one listing and one tester file.
func writeListingDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	writeMetadataFile(t, filepath.Join(dir, "en-US", "title.txt"), "Example")
	writeMetadataFile(t, filepath.Join(dir, "en-US", "short_description.txt"), "Short")
	writeMetadataFile(t, filepath.Join(dir, "en-US", "full_description.txt"), "Full description")
	writeMetadataFile(t, filepath.Join(dir, "testers", "internal.txt"), "qa@googlegroups.com\n")
	return dir
}

func TestPublishAppliesMetadata(t *testing.T) {
	fake := &playtest.Fake{}
	opts := baseOptions(writeAAB(t))
	opts.MetadataDir = writeListingDir(t)

	result, err := app.NewPublisher(fake, nil).Publish(context.Background(), opts)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(fake.Listings) != 1 {
		t.Fatalf("listings = %d, want 1", len(fake.Listings))
	}
	listing := fake.Listings[0]
	if listing.Language != "en-US" || listing.Title != "Example" || listing.FullDescription != "Full description" {
		t.Errorf("unexpected listing: %+v", listing)
	}
	if len(fake.TesterUpdates) != 1 || fake.TesterUpdates[0].Track != "internal" {
		t.Fatalf("tester updates = %+v, want internal", fake.TesterUpdates)
	}
	if fake.TesterUpdates[0].GoogleGroups[0] != "qa@googlegroups.com" {
		t.Errorf("groups = %v", fake.TesterUpdates[0].GoogleGroups)
	}
	if result.Metadata == nil || result.Metadata.Listings != 1 ||
		result.Metadata.Images != 0 || result.Metadata.Testers != 1 {
		t.Fatalf("metadata result = %+v", result.Metadata)
	}

	// Metadata must be written before the commit that applies it.
	commit := indexOf(fake.Calls, "CommitEdit")
	if commit < 0 {
		t.Fatalf("no commit in %v", fake.Calls)
	}
	for _, call := range []string{"UpdateListing", "UpdateTesters"} {
		if idx := indexOf(fake.Calls, call); idx < 0 || idx > commit {
			t.Fatalf("%s at %d, commit at %d: %v", call, idx, commit, fake.Calls)
		}
	}
}

func TestPublishAppliesImages(t *testing.T) {
	fake := &playtest.Fake{}
	opts := baseOptions(writeAAB(t))
	dir := writeListingDir(t)
	writeMetadataImage(t, filepath.Join(dir, "en-US", "images", "icon.png"), 512, 512)
	writeMetadataImage(t, filepath.Join(dir, "en-US", "images", "phoneScreenshots", "01.png"), 1080, 1920)
	writeMetadataImage(t, filepath.Join(dir, "en-US", "images", "phoneScreenshots", "02.png"), 1080, 1920)
	opts.MetadataDir = dir

	result, err := app.NewPublisher(fake, nil).Publish(context.Background(), opts)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(fake.ClearedImages) != 2 {
		t.Fatalf("cleared images = %v, want appIcon and phoneScreenshots", fake.ClearedImages)
	}
	if len(fake.UploadedImages) != 3 {
		t.Fatalf("uploaded images = %v, want 3", fake.UploadedImages)
	}
	if result.Metadata == nil || result.Metadata.Images != 3 {
		t.Fatalf("metadata result = %+v", result.Metadata)
	}
}

func TestPublishReadsNotesFromMetadataDir(t *testing.T) {
	fake := &playtest.Fake{}
	opts := baseOptions(writeAAB(t))
	dir := writeListingDir(t)
	writeMetadataFile(t, filepath.Join(dir, "en-US", "changelogs", "42.txt"), "Metadata fixes")
	opts.MetadataDir = dir

	if _, err := app.NewPublisher(fake, nil).Publish(context.Background(), opts); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	notes := fake.UpdatedReleases[0].ReleaseNotes
	if len(notes) != 1 || notes[0].Text != "Metadata fixes" {
		t.Fatalf("notes = %+v, want the metadata changelog", notes)
	}
}

func TestPublishRejectsMetadataAndChangelogDirs(t *testing.T) {
	fake := &playtest.Fake{}
	opts := baseOptions(writeAAB(t))
	opts.MetadataDir = writeListingDir(t)
	opts.ChangelogDir = t.TempDir()

	_, err := app.NewPublisher(fake, nil).Publish(context.Background(), opts)
	if code := exitcode.From(err); code != exitcode.Usage {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("API was called for invalid input: %v", fake.Calls)
	}
}

func TestPublishMetadataFailureKeepsPlayUnchanged(t *testing.T) {
	fake := &playtest.Fake{
		UpdateListingFunc: func(context.Context, string, string, play.Listing) error {
			return exitcode.Errorf(exitcode.Rejected, "listing rejected")
		},
	}
	opts := baseOptions(writeAAB(t))
	opts.MetadataDir = writeListingDir(t)

	_, err := app.NewPublisher(fake, nil).Publish(context.Background(), opts)
	if code := exitcode.From(err); code != exitcode.Rejected {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Rejected, err)
	}
	if len(fake.DeletedEditIDs) != 1 {
		t.Fatalf("edit was not cleaned up: %v", fake.DeletedEditIDs)
	}
	if len(fake.CommitOptions) != 0 {
		t.Fatalf("failed metadata was committed: %v", fake.CommitOptions)
	}
}

func indexOf(values []string, wanted string) int {
	for i, value := range values {
		if value == wanted {
			return i
		}
	}
	return -1
}
