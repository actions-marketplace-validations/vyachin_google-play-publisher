package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
	"github.com/vyachin/google-play-publisher/internal/storemeta"
)

func pullPNGBytes(t *testing.T, width, height int) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func newCLIPullFake(t *testing.T) *playtest.Fake {
	t.Helper()

	icon := pullPNGBytes(t, 512, 512)
	phone := pullPNGBytes(t, 1080, 1920)

	return &playtest.Fake{
		StoreListingsFunc: func(context.Context, string, string) ([]play.Listing, error) {
			return []play.Listing{{
				Language:         "en-US",
				Title:            "Example",
				ShortDescription: "Short",
				FullDescription:  "Full description",
			}}, nil
		},
		StoreImagesFunc: func(_ context.Context, _, _, _, imageType string) ([]play.Image, error) {
			if imageType == storemeta.ImageAppIcon {
				return []play.Image{{URL: "https://images.example/icon"}}, nil
			}
			return nil, nil
		},
		DownloadImageFunc: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, "icon") {
				return icon, nil
			}
			return phone, nil
		},
		AllTracksFunc: func(context.Context, string, string) ([]play.TrackState, error) {
			return []play.TrackState{{
				Track: "internal",
				Releases: []play.Release{{
					VersionCodes: []int64{42},
					ReleaseNotes: []play.LocalizedText{{Language: "en-US", Text: "Fixes"}},
				}},
			}}, nil
		},
		TesterGroupsFunc: func(_ context.Context, _, _, track string) (play.Testers, error) {
			return play.Testers{Track: track, GoogleGroups: []string{"qa@googlegroups.com"}}, nil
		},
	}
}

func TestPullJSON(t *testing.T) {
	fake := newCLIPullFake(t)
	target := filepath.Join(t.TempDir(), "metadata")

	out, errOut, code := runWithAPI(t, fake, "--json", "pull",
		"-p", "com.example.app", "--metadata-dir", target)
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want 0: %s", code, errOut)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out)
	}
	if got["ok"] != true || got["command"] != "pull" || got["package"] != "com.example.app" {
		t.Fatalf("unexpected output: %v", got)
	}
	if got["metadataDir"] != target || got["track"] != "internal" {
		t.Fatalf("unexpected output: %v", got)
	}
	if got["listings"] != float64(1) || got["images"] != float64(1) ||
		got["changelogs"] != float64(1) || got["testers"] != float64(1) {
		t.Fatalf("counts = %v", got)
	}
	codes, ok := got["versionCodes"].([]any)
	if !ok || len(codes) != 1 || codes[0] != float64(42) {
		t.Fatalf("versionCodes = %#v", got["versionCodes"])
	}
	if len(fake.DeletedEditIDs) != 1 {
		t.Fatalf("edit was not deleted: %v", fake.DeletedEditIDs)
	}
	if _, err := storemeta.Read(target); err != nil {
		t.Fatalf("Read pulled tree: %v", err)
	}
}

func TestPullTextOutput(t *testing.T) {
	fake := newCLIPullFake(t)
	target := filepath.Join(t.TempDir(), "metadata")

	out, errOut, code := runWithAPI(t, fake, "pull",
		"-p", "com.example.app", "--metadata-dir", target)
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want 0: %s", code, errOut)
	}
	for _, want := range []string{
		"pulled com.example.app into " + target,
		"listings: 1",
		"images: 1",
		"changelogs: 1",
		"testers: 1",
		"track: internal (version codes 42)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestPullRejectsNonEmptyTargetBeforeAPI(t *testing.T) {
	fake := newCLIPullFake(t)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}

	_, errOut, code := runWithAPI(t, fake, "pull", "-p", "com.example.app", "--metadata-dir", target)
	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d: %s", code, exitcode.Usage, errOut)
	}
	if !strings.Contains(errOut, "not empty") || !strings.Contains(errOut, "--force") {
		t.Fatalf("unexpected error: %s", errOut)
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("API was called for invalid input: %v", fake.Calls)
	}
}

func TestPullForceReplacesExistingTree(t *testing.T) {
	fake := newCLIPullFake(t)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}

	_, errOut, code := runWithAPI(t, fake, "pull",
		"-p", "com.example.app", "--metadata-dir", target, "--force")
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want 0: %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(target, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old file still present: %v", err)
	}
}

func TestPullRequiresPackage(t *testing.T) {
	fake := newCLIPullFake(t)

	_, errOut, code := runWithAPI(t, fake, "pull", "--metadata-dir", filepath.Join(t.TempDir(), "metadata"))
	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d: %s", code, exitcode.Usage, errOut)
	}
	if !strings.Contains(errOut, "--package is required") {
		t.Fatalf("unexpected error: %s", errOut)
	}
}

func TestPullAliasDownload(t *testing.T) {
	fake := newCLIPullFake(t)
	target := filepath.Join(t.TempDir(), "metadata")

	_, errOut, code := runWithAPI(t, fake, "download", "-p", "com.example.app", "--metadata-dir", target)
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want 0: %s", code, errOut)
	}
}
