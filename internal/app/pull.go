package app

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/release"
	"github.com/vyachin/google-play-publisher/internal/storemeta"
)

// PullOptions describe one read-only download of the store metadata.
type PullOptions struct {
	Package     string
	MetadataDir string
	Track       string
	VersionCode int64
	// Force replaces a non-empty metadata directory. Without it the target
	// must be absent or empty, so a mistyped path cannot destroy a tree.
	Force bool
}

// PullResult describes the tree written by Pull.
type PullResult struct {
	Package      string
	MetadataDir  string
	Track        string
	VersionCodes []int64
	Listings     int
	Images       int
	Changelogs   int
	Testers      int
}

// ValidatePullOptions checks every local input before an API client is built.
// Pull validates again so direct app callers get the same errors.
func ValidatePullOptions(opts PullOptions) error {
	_, err := normalizePullOptions(opts)
	return err
}

func normalizePullOptions(opts PullOptions) (PullOptions, error) {
	pkg := strings.TrimSpace(opts.Package)
	if pkg == "" {
		return PullOptions{}, exitcode.Errorf(exitcode.Usage, "--package is required")
	}
	track, err := release.ValidateTarget(pkg, opts.Track)
	if err != nil {
		return PullOptions{}, err
	}

	dir := strings.TrimSpace(opts.MetadataDir)
	if dir == "" {
		return PullOptions{}, exitcode.Errorf(exitcode.Usage, "--metadata-dir is required")
	}
	dir = filepath.Clean(dir)
	if opts.VersionCode < 0 {
		return PullOptions{}, exitcode.Errorf(exitcode.Usage,
			"--version-code must not be negative, got %d", opts.VersionCode)
	}
	if err := storemeta.CheckTarget(dir, opts.Force); err != nil {
		return PullOptions{}, err
	}

	opts.Package = pkg
	opts.Track = track
	opts.MetadataDir = dir
	return opts, nil
}

// Pull downloads store listings, images, tester groups and release notes into
// a Play-style metadata directory. It creates a read-only edit, never commits
// it, and deletes it before returning. The tree is staged and validated before
// it replaces the target, so a failed pull leaves the previous tree untouched.
func Pull(ctx context.Context, api play.API, opts PullOptions) (PullResult, error) {
	opts, err := normalizePullOptions(opts)
	if err != nil {
		return PullResult{}, err
	}

	writer, err := storemeta.NewWriter(opts.MetadataDir, opts.Force)
	if err != nil {
		return PullResult{}, err
	}
	defer writer.Abort()

	editID, err := api.CreateEdit(ctx, opts.Package)
	if err != nil {
		return PullResult{}, err
	}
	defer cleanupPullEdit(ctx, api, opts.Package, editID)

	result := PullResult{
		Package:      opts.Package,
		MetadataDir:  opts.MetadataDir,
		Track:        opts.Track,
		VersionCodes: []int64{},
	}

	listings, err := api.StoreListings(ctx, opts.Package, editID)
	if err != nil {
		return PullResult{}, err
	}
	for _, listing := range listings {
		if err := writer.AddListing(storemeta.Listing{
			Language:         listing.Language,
			Title:            listing.Title,
			ShortDescription: listing.ShortDescription,
			FullDescription:  listing.FullDescription,
			Video:            listing.Video,
		}); err != nil {
			return PullResult{}, err
		}
		result.Listings++

		for _, imageType := range storemeta.ImageTypes() {
			images, err := api.StoreImages(ctx, opts.Package, editID, listing.Language, imageType)
			if err != nil {
				return PullResult{}, err
			}
			for _, image := range images {
				content, err := api.DownloadImage(ctx, image.URL)
				if err != nil {
					return PullResult{}, err
				}
				if err := writer.AddImage(listing.Language, imageType, content); err != nil {
					return PullResult{}, err
				}
				result.Images++
			}
		}
	}

	tracks, err := api.AllTracks(ctx, opts.Package, editID)
	if err != nil {
		return PullResult{}, err
	}
	for _, track := range tracks {
		// Play exposes testers only for testing tracks. production (and its
		// form-factor variants) answers 400, so skip it instead of failing a
		// metadata pull that otherwise succeeded.
		if !isTestingTrack(track.Track) {
			continue
		}
		testers, err := api.TesterGroups(ctx, opts.Package, editID, track.Track)
		if err != nil {
			if exitcode.From(err) == exitcode.NotFound {
				continue
			}
			return PullResult{}, err
		}
		if len(testers.GoogleGroups) == 0 {
			continue
		}
		if testers.Track == "" {
			testers.Track = track.Track
		}
		if err := writer.AddTesters(storemeta.Testers{
			Track:        testers.Track,
			GoogleGroups: testers.GoogleGroups,
		}); err != nil {
			return PullResult{}, err
		}
		result.Testers++
	}

	track, found := findTrack(tracks, opts.Track)
	switch {
	case found:
		releases, codes, err := selectPullReleases(track, opts.VersionCode)
		if err != nil {
			return PullResult{}, err
		}
		result.VersionCodes = codes
		for _, selected := range releases {
			for _, note := range selected.release.ReleaseNotes {
				language := strings.TrimSpace(note.Language)
				if language == "" || strings.TrimSpace(note.Text) == "" {
					continue
				}
				for _, code := range selected.codes {
					if err := writer.AddChangelog(language, code, note.Text); err != nil {
						return PullResult{}, err
					}
					result.Changelogs++
				}
			}
		}
	case opts.VersionCode > 0:
		return PullResult{}, exitcode.WithHint(
			exitcode.Errorf(exitcode.NotFound, "track %s was not found for %s", opts.Track, opts.Package),
			"run status --package "+opts.Package+" --track "+opts.Track+" to inspect the track",
		)
	}

	if err := writer.Commit(); err != nil {
		return PullResult{}, err
	}
	return result, nil
}

// cleanupPullEdit deletes the read-only edit. A signal must not send another
// request, so the edit is left for Play to expire; an expired --timeout still
// runs the bounded cleanup.
func cleanupPullEdit(ctx context.Context, api play.API, pkg, editID string) {
	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = api.DeleteEdit(cleanupCtx, pkg, editID)
}

// isTestingTrack reports whether Play accepts testers.get for the track.
// Production tracks have no tester configuration; form-factor tracks use the
// same "<formFactor>:production" suffix.
func isTestingTrack(name string) bool {
	base := name
	if index := strings.LastIndex(name, ":"); index >= 0 {
		base = name[index+1:]
	}
	return base != "production"
}

func findTrack(tracks []play.TrackState, name string) (play.TrackState, bool) {
	for _, track := range tracks {
		if track.Track == name {
			return track, true
		}
	}
	return play.TrackState{}, false
}

// selectedPullRelease is one release and the version codes whose changelog
// files should be written from its notes.
type selectedPullRelease struct {
	release play.Release
	codes   []int64
}

// selectPullReleases chooses the release notes to pull. With an explicit
// version code only that version is selected. Without one, notes from every
// release on the track are pulled, so a full metadata snapshot contains the
// history of the track, not only its newest release.
func selectPullReleases(track play.TrackState, versionCode int64) ([]selectedPullRelease, []int64, error) {
	if versionCode > 0 {
		for _, rel := range track.Releases {
			if containsVersionCode(rel.VersionCodes, versionCode) {
				return []selectedPullRelease{{release: rel, codes: []int64{versionCode}}}, []int64{versionCode}, nil
			}
		}
		return nil, nil, exitcode.WithHint(
			exitcode.Errorf(exitcode.NotFound,
				"version code %d is not on track %s", versionCode, track.Track),
			"omit --version-code to pull every release, or run status to list the version codes",
		)
	}

	seen := make(map[int64]bool)
	releases := make([]selectedPullRelease, 0, len(track.Releases))
	codes := make([]int64, 0)
	for _, rel := range track.Releases {
		selected := make([]int64, 0, len(rel.VersionCodes))
		for _, code := range sortedVersionCodes(rel.VersionCodes) {
			if seen[code] {
				continue
			}
			seen[code] = true
			selected = append(selected, code)
			codes = append(codes, code)
		}
		if len(selected) > 0 {
			releases = append(releases, selectedPullRelease{release: rel, codes: selected})
		}
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	return releases, codes, nil
}

func sortedVersionCodes(codes []int64) []int64 {
	out := append([]int64(nil), codes...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func containsVersionCode(codes []int64, wanted int64) bool {
	for _, code := range codes {
		if code == wanted {
			return true
		}
	}
	return false
}
