package storemeta_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/storemeta"
)

func encodeImage(t *testing.T, width, height int, format string) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})

	var buf bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	default:
		err = png.Encode(&buf, img)
	}
	if err != nil {
		t.Fatalf("encode image: %v", err)
	}
	return buf.Bytes()
}

func TestWriterWritesACompleteTree(t *testing.T) {
	target := filepath.Join(t.TempDir(), "metadata")

	writer, err := storemeta.NewWriter(target, false)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer writer.Abort()

	if err := writer.AddListing(storemeta.Listing{
		Language:         "en-US",
		Title:            "Example",
		ShortDescription: "Short",
		FullDescription:  "Full description",
		Video:            "https://www.youtube.com/watch?v=abc",
	}); err != nil {
		t.Fatalf("AddListing: %v", err)
	}
	for _, image := range []struct {
		imageType string
		width     int
		height    int
	}{
		{storemeta.ImageAppIcon, 512, 512},
		{storemeta.ImageFeatureGraphic, 1024, 500},
	} {
		if err := writer.AddImage("en-US", image.imageType, encodeImage(t, image.width, image.height, "png")); err != nil {
			t.Fatalf("AddImage(%s): %v", image.imageType, err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := writer.AddImage("en-US", "phoneScreenshots", encodeImage(t, 1080, 1920, "png")); err != nil {
			t.Fatalf("AddImage(screenshot): %v", err)
		}
	}
	if err := writer.AddTesters(storemeta.Testers{
		Track:        "internal",
		GoogleGroups: []string{"qa@googlegroups.com", "beta@googlegroups.com"},
	}); err != nil {
		t.Fatalf("AddTesters: %v", err)
	}
	if err := writer.AddChangelog("en-US", 42, "Fixes"); err != nil {
		t.Fatalf("AddChangelog: %v", err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	meta, err := storemeta.Read(target)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(meta.Listings) != 1 || meta.Listings[0].Language != "en-US" {
		t.Fatalf("listings = %+v", meta.Listings)
	}
	if meta.Listings[0].Video != "https://www.youtube.com/watch?v=abc" {
		t.Errorf("video = %q", meta.Listings[0].Video)
	}

	counts := make(map[string]int)
	for _, set := range meta.Images {
		counts[set.Type] += len(set.Files)
	}
	if counts[storemeta.ImageAppIcon] != 1 ||
		counts[storemeta.ImageFeatureGraphic] != 1 ||
		counts["phoneScreenshots"] != 2 {
		t.Fatalf("image counts = %v", counts)
	}
	if len(meta.Testers) != 1 || len(meta.Testers[0].GoogleGroups) != 2 {
		t.Fatalf("testers = %+v", meta.Testers)
	}

	changelog, err := os.ReadFile(filepath.Join(target, "en-US", "changelogs", "42.txt"))
	if err != nil {
		t.Fatalf("read changelog: %v", err)
	}
	if strings.TrimSpace(string(changelog)) != "Fixes" {
		t.Errorf("changelog = %q", changelog)
	}
	if _, err := os.Stat(filepath.Join(target, "en-US", "images", "icon.png")); err != nil {
		t.Errorf("icon file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "en-US", "images", "phoneScreenshots", "001.png")); err != nil {
		t.Errorf("first screenshot file: %v", err)
	}
}

func TestWriterRejectsNonEmptyTargetWithoutForce(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}

	_, err := storemeta.NewWriter(target, false)
	if code := exitcode.From(err); code != exitcode.Usage {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
	}
	if !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriterForceReplacesExistingTree(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}

	writer, err := storemeta.NewWriter(target, true)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer writer.Abort()
	if err := writer.AddListing(storemeta.Listing{
		Language:         "en-US",
		Title:            "Example",
		ShortDescription: "Short",
		FullDescription:  "Full description",
	}); err != nil {
		t.Fatalf("AddListing: %v", err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old file still present: %v", err)
	}
	if _, err := storemeta.Read(target); err != nil {
		t.Fatalf("Read committed tree: %v", err)
	}
}

func TestWriterAbortKeepsExistingTree(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}

	writer, err := storemeta.NewWriter(target, true)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := writer.AddListing(storemeta.Listing{
		Language:         "en-US",
		Title:            "Example",
		ShortDescription: "Short",
		FullDescription:  "Full description",
	}); err != nil {
		t.Fatalf("AddListing: %v", err)
	}
	writer.Abort()

	if data, err := os.ReadFile(filepath.Join(target, "old.txt")); err != nil || string(data) != "old" {
		t.Fatalf("old file = %q, %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatalf("read parent: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".pull-") {
			t.Fatalf("staging directory %q was not removed", entry.Name())
		}
	}
}

func TestWriterCommitRejectsInvalidPulledData(t *testing.T) {
	target := filepath.Join(t.TempDir(), "metadata")

	writer, err := storemeta.NewWriter(target, false)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer writer.Abort()
	if err := writer.AddListing(storemeta.Listing{
		Language:         "en-US",
		Title:            "Example",
		ShortDescription: "Short",
		FullDescription:  "Full description",
	}); err != nil {
		t.Fatalf("AddListing: %v", err)
	}
	if err := writer.AddImage("en-US", storemeta.ImageAppIcon, encodeImage(t, 256, 256, "png")); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	err = writer.Commit()
	if code := exitcode.From(err); code != exitcode.Usage {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("target exists after failed commit: %v", statErr)
	}
}

func TestWriterAcceptsJPEGScreenshots(t *testing.T) {
	target := filepath.Join(t.TempDir(), "metadata")
	writer, err := storemeta.NewWriter(target, false)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer writer.Abort()
	if err := writer.AddListing(storemeta.Listing{
		Language:         "en-US",
		Title:            "Example",
		ShortDescription: "Short",
		FullDescription:  "Full description",
	}); err != nil {
		t.Fatalf("AddListing: %v", err)
	}
	if err := writer.AddImage("en-US", "phoneScreenshots", encodeImage(t, 1080, 1920, "jpeg")); err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "en-US", "images", "phoneScreenshots", "001.jpg")); err != nil {
		t.Fatalf("jpeg screenshot: %v", err)
	}
}

func TestWriterRejectsDuplicateSingularImage(t *testing.T) {
	writer, err := storemeta.NewWriter(filepath.Join(t.TempDir(), "metadata"), false)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer writer.Abort()

	content := encodeImage(t, 512, 512, "png")
	if err := writer.AddImage("en-US", storemeta.ImageAppIcon, content); err != nil {
		t.Fatalf("first AddImage: %v", err)
	}
	err = writer.AddImage("en-US", storemeta.ImageAppIcon, content)
	if code := exitcode.From(err); code != exitcode.Usage {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
	}
}

func TestCheckTargetRejectsUnsafeDirectories(t *testing.T) {
	for _, dir := range []string{".", "..", string(filepath.Separator)} {
		t.Run(dir, func(t *testing.T) {
			err := storemeta.CheckTarget(dir, true)
			if code := exitcode.From(err); code != exitcode.Usage {
				t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
			}
		})
	}
}

func TestWriterNormalizesTrailingSeparator(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "metadata") + string(os.PathSeparator)

	writer, err := storemeta.NewWriter(target, false)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer writer.Abort()
	if err := writer.AddListing(storemeta.Listing{
		Language:         "en-US",
		Title:            "Example",
		ShortDescription: "Short",
		FullDescription:  "Full description",
	}); err != nil {
		t.Fatalf("AddListing: %v", err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if _, err := storemeta.Read(filepath.Join(base, "metadata")); err != nil {
		t.Fatalf("Read: %v", err)
	}
}

func TestWriterAcceptsEmptyExistingTarget(t *testing.T) {
	target := t.TempDir()

	writer, err := storemeta.NewWriter(target, false)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer writer.Abort()
	if err := writer.AddListing(storemeta.Listing{
		Language:         "en-US",
		Title:            "Example",
		ShortDescription: "Short",
		FullDescription:  "Full description",
	}); err != nil {
		t.Fatalf("AddListing: %v", err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if _, err := storemeta.Read(target); err != nil {
		t.Fatalf("Read: %v", err)
	}
}
