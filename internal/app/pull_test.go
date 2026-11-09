package app_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
	"github.com/vyachin/google-play-publisher/internal/storemeta"
)

func pullPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

type pullFixture struct {
	fake    *playtest.Fake
	icon    []byte
	feature []byte
	phone   []byte
}

func newPullFixture() *pullFixture {
	f := &pullFixture{
		icon:    nil,
		feature: nil,
		phone:   nil,
	}
	f.fake = &playtest.Fake{}
	return f
}

func (f *pullFixture) install(t *testing.T) {
	t.Helper()

	f.icon = pullPNG(t, 512, 512)
	f.feature = pullPNG(t, 1024, 500)
	f.phone = pullPNG(t, 1080, 1920)

	f.fake.StoreListingsFunc = func(context.Context, string, string) ([]play.Listing, error) {
		return []play.Listing{{
			Language:         "en-US",
			Title:            "Example",
			ShortDescription: "Short",
			FullDescription:  "Full description",
			Video:            "https://www.youtube.com/watch?v=abc",
		}}, nil
	}
	f.fake.StoreImagesFunc = func(_ context.Context, _, _, _, imageType string) ([]play.Image, error) {
		switch imageType {
		case storemeta.ImageAppIcon:
			return []play.Image{{URL: "https://images.example/icon"}}, nil
		case storemeta.ImageFeatureGraphic:
			return []play.Image{{URL: "https://images.example/feature"}}, nil
		case "phoneScreenshots":
			return []play.Image{
				{URL: "https://images.example/phone-1"},
				{URL: "https://images.example/phone-2"},
			}, nil
		default:
			return nil, nil
		}
	}
	f.fake.DownloadImageFunc = func(_ context.Context, url string) ([]byte, error) {
		switch {
		case strings.Contains(url, "icon"):
			return f.icon, nil
		case strings.Contains(url, "feature"):
			return f.feature, nil
		default:
			return f.phone, nil
		}
	}
	f.fake.AllTracksFunc = func(context.Context, string, string) ([]play.TrackState, error) {
		return []play.TrackState{
			{
				Track: "internal",
				Releases: []play.Release{{
					VersionCodes: []int64{42},
					Status:       play.StatusCompleted,
					ReleaseNotes: []play.LocalizedText{{Language: "en-US", Text: "Fixes"}},
				}},
			},
			{Track: "beta"},
			{Track: "production"},
		}, nil
	}
	f.fake.TesterGroupsFunc = func(_ context.Context, _, _, track string) (play.Testers, error) {
		if track != "internal" {
			return play.Testers{}, exitcode.Errorf(exitcode.NotFound, "no testers")
		}
		return play.Testers{Track: track, GoogleGroups: []string{"qa@googlegroups.com"}}, nil
	}
}

func TestPullWritesCompleteMetadataTree(t *testing.T) {
	fixture := newPullFixture()
	fixture.install(t)
	target := filepath.Join(t.TempDir(), "metadata")

	result, err := app.Pull(context.Background(), fixture.fake, app.PullOptions{
		Package:     "com.example.app",
		MetadataDir: target,
		Track:       "internal",
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	if result.Listings != 1 || result.Images != 4 || result.Changelogs != 1 || result.Testers != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Tracks) != 1 || result.Tracks[0] != "internal" {
		t.Fatalf("tracks = %v", result.Tracks)
	}
	if len(result.VersionCodes) != 1 || result.VersionCodes[0] != 42 {
		t.Fatalf("version codes = %v", result.VersionCodes)
	}

	meta, err := storemeta.Read(target)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(meta.Listings) != 1 || meta.Listings[0].Title != "Example" {
		t.Fatalf("listings = %+v", meta.Listings)
	}
	if len(meta.Testers) != 1 || meta.Testers[0].Track != "internal" {
		t.Fatalf("testers = %+v", meta.Testers)
	}

	for _, path := range []string{
		filepath.Join(target, "en-US", "title.txt"),
		filepath.Join(target, "en-US", "video.txt"),
		filepath.Join(target, "en-US", "images", "icon.png"),
		filepath.Join(target, "en-US", "images", "featureGraphic.png"),
		filepath.Join(target, "en-US", "images", "phoneScreenshots", "001.png"),
		filepath.Join(target, "en-US", "images", "phoneScreenshots", "002.png"),
		filepath.Join(target, "en-US", "changelogs", "42.txt"),
		filepath.Join(target, "testers", "internal.txt"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", path, err)
		}
	}

	if len(fixture.fake.DeletedEditIDs) != 1 || fixture.fake.DeletedEditIDs[0] != "edit-1" {
		t.Fatalf("deleted edits = %v", fixture.fake.DeletedEditIDs)
	}
	for _, track := range fixture.fake.ReadTesterTracks {
		if track == "production" {
			t.Fatalf("testers were read for production: %v", fixture.fake.ReadTesterTracks)
		}
	}
	for _, call := range fixture.fake.Calls {
		if call == "CommitEdit" {
			t.Fatalf("pull committed an edit: %v", fixture.fake.Calls)
		}
	}
}

func TestPullChangelogsForSelectedOrAllReleases(t *testing.T) {
	fixture := newPullFixture()
	fixture.install(t)
	target := filepath.Join(t.TempDir(), "metadata")

	fixture.fake.AllTracksFunc = func(context.Context, string, string) ([]play.TrackState, error) {
		return []play.TrackState{
			{
				Track: "internal",
				Releases: []play.Release{
					{VersionCodes: []int64{41}, ReleaseNotes: []play.LocalizedText{{Language: "en-US", Text: "Old"}}},
					{VersionCodes: []int64{42, 43}, ReleaseNotes: []play.LocalizedText{{Language: "en-US", Text: "New"}}},
				},
			},
			{
				Track: "beta",
				Releases: []play.Release{
					{VersionCodes: []int64{36}, ReleaseNotes: []play.LocalizedText{{Language: "en-US", Text: "Beta"}}},
				},
			},
		}, nil
	}

	result, err := app.Pull(context.Background(), fixture.fake, app.PullOptions{
		Package:     "com.example.app",
		MetadataDir: target,
		Track:       "internal",
		VersionCode: 41,
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(result.VersionCodes) != 1 || result.VersionCodes[0] != 41 {
		t.Fatalf("version codes = %v", result.VersionCodes)
	}
	data, err := os.ReadFile(filepath.Join(target, "en-US", "changelogs", "41.txt"))
	if err != nil || strings.TrimSpace(string(data)) != "Old" {
		t.Fatalf("changelog = %q, %v", data, err)
	}
	for _, code := range []string{"42", "43"} {
		if _, err := os.Stat(filepath.Join(target, "en-US", "changelogs", code+".txt")); !os.IsNotExist(err) {
			t.Fatalf("changelog %s should not exist for an explicit version code: %v", code, err)
		}
	}

	target = filepath.Join(t.TempDir(), "metadata")
	result, err = app.Pull(context.Background(), fixture.fake, app.PullOptions{
		Package:     "com.example.app",
		MetadataDir: target,
	})
	if err != nil {
		t.Fatalf("Pull default: %v", err)
	}
	if len(result.Tracks) != 2 || result.Tracks[0] != "beta" || result.Tracks[1] != "internal" {
		t.Fatalf("default tracks = %v", result.Tracks)
	}
	if len(result.VersionCodes) != 4 || result.VersionCodes[0] != 36 ||
		result.VersionCodes[1] != 41 || result.VersionCodes[2] != 42 || result.VersionCodes[3] != 43 {
		t.Fatalf("default version codes = %v", result.VersionCodes)
	}
	if result.Changelogs != 4 {
		t.Fatalf("changelogs = %d, want 4", result.Changelogs)
	}
	for code, want := range map[string]string{"36": "Beta", "41": "Old", "42": "New", "43": "New"} {
		data, err := os.ReadFile(filepath.Join(target, "en-US", "changelogs", code+".txt"))
		if err != nil || strings.TrimSpace(string(data)) != want {
			t.Fatalf("changelog %s = %q, %v; want %q", code, data, err, want)
		}
	}
}

func TestPullRejectsMissingVersionCode(t *testing.T) {
	fixture := newPullFixture()
	fixture.install(t)

	_, err := app.Pull(context.Background(), fixture.fake, app.PullOptions{
		Package:     "com.example.app",
		MetadataDir: filepath.Join(t.TempDir(), "metadata"),
		Track:       "internal",
		VersionCode: 99,
	})
	if code := exitcode.From(err); code != exitcode.NotFound {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.NotFound, err)
	}
}

func TestPullRejectsNonEmptyTargetBeforeAPI(t *testing.T) {
	fixture := newPullFixture()
	fixture.install(t)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}

	_, err := app.Pull(context.Background(), fixture.fake, app.PullOptions{
		Package:     "com.example.app",
		MetadataDir: target,
	})
	if code := exitcode.From(err); code != exitcode.Usage {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
	}
	if len(fixture.fake.Calls) != 0 {
		t.Fatalf("API was called for invalid input: %v", fixture.fake.Calls)
	}
}

func TestPullForceReplacesExistingTree(t *testing.T) {
	fixture := newPullFixture()
	fixture.install(t)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}

	if _, err := app.Pull(context.Background(), fixture.fake, app.PullOptions{
		Package:     "com.example.app",
		MetadataDir: target,
		Force:       true,
	}); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old file still present: %v", err)
	}
	if _, err := storemeta.Read(target); err != nil {
		t.Fatalf("Read new tree: %v", err)
	}
}

func TestPullDownloadFailureKeepsTargetAndDeletesEdit(t *testing.T) {
	fixture := newPullFixture()
	fixture.install(t)
	target := filepath.Join(t.TempDir(), "metadata")
	fixture.fake.DownloadImageFunc = func(context.Context, string) ([]byte, error) {
		return nil, exitcode.Errorf(exitcode.Transient, "download failed")
	}

	_, err := app.Pull(context.Background(), fixture.fake, app.PullOptions{
		Package:     "com.example.app",
		MetadataDir: target,
	})
	if code := exitcode.From(err); code != exitcode.Transient {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Transient, err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("target exists after failed pull: %v", statErr)
	}
	if len(fixture.fake.DeletedEditIDs) != 1 {
		t.Fatalf("edit was not deleted: %v", fixture.fake.DeletedEditIDs)
	}
}

func TestValidatePullOptions(t *testing.T) {
	tests := []struct {
		name string
		opts app.PullOptions
		want string
	}{
		{
			name: "missing package",
			opts: app.PullOptions{MetadataDir: "metadata"},
			want: "--package is required",
		},
		{
			name: "missing directory",
			opts: app.PullOptions{Package: "com.example.app"},
			want: "--metadata-dir is required",
		},
		{
			name: "invalid track",
			opts: app.PullOptions{Package: "com.example.app", MetadataDir: "metadata", Track: "not a track"},
			want: "invalid track name",
		},
		{
			name: "negative version code",
			opts: app.PullOptions{Package: "com.example.app", MetadataDir: "metadata", VersionCode: -1},
			want: "must not be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := app.ValidatePullOptions(tt.opts)
			if code := exitcode.From(err); code != exitcode.Usage {
				t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestPullMissingTrack(t *testing.T) {
	fixture := newPullFixture()
	fixture.install(t)
	fixture.fake.AllTracksFunc = func(context.Context, string, string) ([]play.TrackState, error) {
		return []play.TrackState{{Track: "beta"}}, nil
	}

	_, err := app.Pull(context.Background(), fixture.fake, app.PullOptions{
		Package:     "com.example.app",
		MetadataDir: filepath.Join(t.TempDir(), "metadata"),
		Track:       "internal",
	})
	if code := exitcode.From(err); code != exitcode.NotFound {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.NotFound, err)
	}
}

func TestPullRejectsAmbiguousVersionCode(t *testing.T) {
	fixture := newPullFixture()
	fixture.install(t)
	fixture.fake.AllTracksFunc = func(context.Context, string, string) ([]play.TrackState, error) {
		return []play.TrackState{
			{Track: "internal", Releases: []play.Release{{VersionCodes: []int64{42}}}},
			{Track: "beta", Releases: []play.Release{{VersionCodes: []int64{42}}}},
		}, nil
	}

	_, err := app.Pull(context.Background(), fixture.fake, app.PullOptions{
		Package:     "com.example.app",
		MetadataDir: filepath.Join(t.TempDir(), "metadata"),
		VersionCode: 42,
	})
	if code := exitcode.From(err); code != exitcode.Usage {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
	}
	var coded *exitcode.Error
	if !errors.As(err, &coded) || !strings.Contains(coded.Hint, "--track") {
		t.Fatalf("error does not suggest --track: %v", err)
	}
}
