package storemeta_test

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/storemeta"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writePNG(t *testing.T, path string, width, height int) {
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

func writeListing(t *testing.T, dir, locale string) {
	t.Helper()

	writeFile(t, filepath.Join(dir, locale, "title.txt"), "Example")
	writeFile(t, filepath.Join(dir, locale, "short_description.txt"), "Short")
	writeFile(t, filepath.Join(dir, locale, "full_description.txt"), "Full description")
}

func TestReadFullDirectory(t *testing.T) {
	dir := t.TempDir()
	writeListing(t, dir, "en-US")
	writeFile(t, filepath.Join(dir, "en-US", "video.txt"), "https://www.youtube.com/watch?v=abc")
	writePNG(t, filepath.Join(dir, "en-US", "images", "icon.png"), 512, 512)
	writePNG(t, filepath.Join(dir, "en-US", "images", "featureGraphic.png"), 1024, 500)
	writePNG(t, filepath.Join(dir, "en-US", "images", "phoneScreenshots", "01.png"), 1080, 1920)
	writePNG(t, filepath.Join(dir, "en-US", "images", "phoneScreenshots", "02.png"), 1080, 1920)
	writeFile(t, filepath.Join(dir, "testers", "internal.txt"), "qa@googlegroups.com\n\nqa@googlegroups.com\nbeta@googlegroups.com\n")
	// Changelogs live in the same tree and must not be picked up as metadata.
	writeFile(t, filepath.Join(dir, "en-US", "changelogs", "42.txt"), "Fixes")

	meta, err := storemeta.Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(meta.Listings) != 1 {
		t.Fatalf("listings = %d, want 1", len(meta.Listings))
	}
	listing := meta.Listings[0]
	if listing.Language != "en-US" || listing.Title != "Example" || listing.ShortDescription != "Short" ||
		listing.FullDescription != "Full description" || listing.Video == "" {
		t.Errorf("unexpected listing: %+v", listing)
	}

	types := make(map[string]int, len(meta.Images))
	for _, set := range meta.Images {
		types[set.Type] += len(set.Files)
	}
	want := map[string]int{
		storemeta.ImageAppIcon:        1,
		storemeta.ImageFeatureGraphic: 1,
		"phoneScreenshots":            2,
	}
	for imageType, count := range want {
		if types[imageType] != count {
			t.Errorf("%s images = %d, want %d", imageType, types[imageType], count)
		}
	}

	if len(meta.Testers) != 1 {
		t.Fatalf("testers = %d, want 1", len(meta.Testers))
	}
	if meta.Testers[0].Track != "internal" {
		t.Errorf("track = %q, want internal", meta.Testers[0].Track)
	}
	groups := meta.Testers[0].GoogleGroups
	if len(groups) != 2 || groups[0] != "beta@googlegroups.com" || groups[1] != "qa@googlegroups.com" {
		t.Errorf("groups = %v, want sorted and de-duplicated", groups)
	}
}

func TestReadEmptyScreenshotDirectoryClearsType(t *testing.T) {
	dir := t.TempDir()
	writeListing(t, dir, "en-US")
	if err := os.MkdirAll(filepath.Join(dir, "en-US", "images", "phoneScreenshots"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	meta, err := storemeta.Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(meta.Images) != 1 || meta.Images[0].Type != "phoneScreenshots" || len(meta.Images[0].Files) != 0 {
		t.Fatalf("images = %+v, want one empty phoneScreenshots set", meta.Images)
	}
}

func TestReadRejectsIncompleteAndInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T, dir string)
		want  string
	}{
		{
			name: "incomplete listing",
			build: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "en-US", "title.txt"), "Example")
			},
			want: "short_description.txt is missing",
		},
		{
			name: "title too long",
			build: func(t *testing.T, dir string) {
				writeListing(t, dir, "en-US")
				writeFile(t, filepath.Join(dir, "en-US", "title.txt"), strings.Repeat("x", 31))
			},
			want: "limit is 30",
		},
		{
			name: "empty description",
			build: func(t *testing.T, dir string) {
				writeListing(t, dir, "en-US")
				writeFile(t, filepath.Join(dir, "en-US", "full_description.txt"), "  \n")
			},
			want: "is empty",
		},
		{
			name: "invalid locale directory",
			build: func(t *testing.T, dir string) {
				writeListing(t, dir, "not a locale")
			},
			want: "invalid locale",
		},
		{
			name: "unknown image file",
			build: func(t *testing.T, dir string) {
				writeListing(t, dir, "en-US")
				writePNG(t, filepath.Join(dir, "en-US", "images", "splash.png"), 100, 100)
			},
			want: "unknown image file",
		},
		{
			name: "icon size",
			build: func(t *testing.T, dir string) {
				writeListing(t, dir, "en-US")
				writePNG(t, filepath.Join(dir, "en-US", "images", "icon.png"), 256, 256)
			},
			want: "Play requires 512x512",
		},
		{
			name: "feature graphic size",
			build: func(t *testing.T, dir string) {
				writeListing(t, dir, "en-US")
				writePNG(t, filepath.Join(dir, "en-US", "images", "featureGraphic.png"), 512, 512)
			},
			want: "Play requires 1024x500",
		},
		{
			name: "not an image",
			build: func(t *testing.T, dir string) {
				writeListing(t, dir, "en-US")
				writeFile(t, filepath.Join(dir, "en-US", "images", "icon.png"), "not a png")
			},
			want: "not a readable PNG or JPEG",
		},
		{
			name: "bad tester line",
			build: func(t *testing.T, dir string) {
				writeListing(t, dir, "en-US")
				writeFile(t, filepath.Join(dir, "testers", "beta.txt"), "not-an-email\n")
			},
			want: "is not an email address",
		},
		{
			name: "empty testers",
			build: func(t *testing.T, dir string) {
				writeListing(t, dir, "en-US")
				writeFile(t, filepath.Join(dir, "testers", "beta.txt"), "\n\n")
			},
			want: "lists no Google Groups",
		},
		{
			name: "video is not a URL",
			build: func(t *testing.T, dir string) {
				writeListing(t, dir, "en-US")
				writeFile(t, filepath.Join(dir, "en-US", "video.txt"), "youtube.com/watch?v=abc")
			},
			want: "is not an http(s) URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.build(t, dir)

			_, err := storemeta.Read(dir)
			if code := exitcode.From(err); code != exitcode.Usage {
				t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestReadEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "en-US", "changelogs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	meta, err := storemeta.Read(dir)
	if err != nil || meta != nil {
		t.Fatalf("meta = %+v, err = %v; want nil, nil", meta, err)
	}
}

func TestReadWithoutDirectory(t *testing.T) {
	meta, err := storemeta.Read("")
	if err != nil || meta != nil {
		t.Fatalf("meta = %+v, err = %v; want nil, nil", meta, err)
	}
}

func TestReadMissingDirectory(t *testing.T) {
	_, err := storemeta.Read(filepath.Join(t.TempDir(), "missing"))
	if code := exitcode.From(err); code != exitcode.Usage {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
	}
}
