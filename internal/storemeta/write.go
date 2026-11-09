package storemeta

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/release"
)

// CheckTarget validates the directory a pull is going to write. The target
// may not exist or be empty; replacing a non-empty tree requires force, so a
// mistyped path cannot delete an unrelated directory.
func CheckTarget(dir string, force bool) error {
	if strings.TrimSpace(dir) == "" {
		return exitcode.Errorf(exitcode.Usage, "--metadata-dir is required")
	}
	clean := filepath.Clean(dir)
	volumeRoot := filepath.VolumeName(clean) + string(filepath.Separator)
	if clean == "." || clean == ".." || clean == string(filepath.Separator) || clean == volumeRoot {
		return exitcode.WithHint(
			exitcode.Errorf(exitcode.Usage, "%s is not a metadata directory", dir),
			"choose a dedicated directory such as metadata or build/metadata",
		)
	}

	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the path passed to --metadata-dir",
		)
	case !info.IsDir():
		return exitcode.Errorf(exitcode.Usage, "%s is not a directory", dir)
	}

	if force {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the path passed to --metadata-dir",
		)
	}
	if len(entries) > 0 {
		return exitcode.WithHint(
			exitcode.Errorf(exitcode.Usage, "metadata directory %s is not empty", dir),
			"choose an empty directory or pass --force to replace it",
		)
	}
	return nil
}

// Writer builds a metadata directory in a staging directory and swaps it into
// place only after every file passed the same validation as Read. A failed
// pull therefore never leaves a half-written tree behind.
type Writer struct {
	target string
	stage  string
	force  bool

	listings    map[string]bool
	single      map[string]bool
	screenshots map[string]int
	testers     map[string]bool
	changelogs  map[string]bool
}

// NewWriter prepares a staging directory next to target. Call Abort when the
// data cannot be fetched; Commit validates and installs the tree.
func NewWriter(target string, force bool) (*Writer, error) {
	target = filepath.Clean(target)
	if err := CheckTarget(target, force); err != nil {
		return nil, err
	}

	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the metadata directory path and permissions",
		)
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(target)+".pull-")
	if err != nil {
		return nil, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the metadata directory path and permissions",
		)
	}
	// MkdirTemp creates 0700; a metadata tree is committed to a repository and
	// must stay readable by the other users and CI steps of the workspace.
	if err := os.Chmod(stage, 0o755); err != nil {
		_ = os.RemoveAll(stage)
		return nil, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the metadata directory path and permissions",
		)
	}

	return &Writer{
		target:      target,
		stage:       stage,
		force:       force,
		listings:    make(map[string]bool),
		single:      make(map[string]bool),
		screenshots: make(map[string]int),
		testers:     make(map[string]bool),
		changelogs:  make(map[string]bool),
	}, nil
}

// Abort removes the staging directory. It is safe to call after Commit.
func (w *Writer) Abort() {
	if w.stage == "" {
		return
	}
	_ = os.RemoveAll(w.stage)
	w.stage = ""
}

// Commit validates the staged tree with Read and installs it at the target.
// A non-empty existing tree is replaced atomically when the writer was created
// with force; without force the replacement is refused.
func (w *Writer) Commit() error {
	if w.stage == "" {
		return exitcode.Errorf(exitcode.Internal, "metadata writer is closed")
	}
	if _, err := Read(w.stage); err != nil {
		return exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, fmt.Errorf("the data returned by Play is not a valid metadata tree: %w", err)),
			"rerun with --debug to inspect the API responses, or edit the generated tree before publishing",
		)
	}
	if err := CheckTarget(w.target, w.force); err != nil {
		return err
	}

	info, err := os.Stat(w.target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.Rename(w.stage, w.target); err != nil {
			return exitcode.WithHint(
				exitcode.Wrap(exitcode.Usage, err),
				"check the metadata directory path and permissions",
			)
		}
		w.stage = ""
		return nil
	case err != nil:
		return exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the metadata directory path and permissions",
		)
	case !info.IsDir():
		return exitcode.Errorf(exitcode.Usage, "%s is not a directory", w.target)
	}

	// Move the old tree aside first: rename cannot replace a non-empty
	// directory, and a failed install must not destroy the previous version.
	backup := w.stage + ".old"
	if err := os.Rename(w.target, backup); err != nil {
		return exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the metadata directory path and permissions",
		)
	}
	if err := os.Rename(w.stage, w.target); err != nil {
		restoreErr := os.Rename(backup, w.target)
		if restoreErr != nil {
			return exitcode.WithHint(
				exitcode.Wrap(exitcode.Usage, fmt.Errorf(
					"install the new metadata tree: %w; restoring %s also failed: %v", err, w.target, restoreErr)),
				"the previous tree is at "+backup+"; restore it manually",
			)
		}
		return exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the metadata directory path and permissions",
		)
	}
	w.stage = ""
	// The target is already correct; a leftover hidden backup is preferable to
	// reporting a failure for a completed pull.
	_ = os.RemoveAll(backup)
	return nil
}

// AddListing writes one localized store listing.
func (w *Writer) AddListing(l Listing) error {
	if err := release.ValidateLocale(l.Language); err != nil {
		return err
	}
	if w.listings[l.Language] {
		return exitcode.Errorf(exitcode.Usage, "Play returned more than one listing for %s", l.Language)
	}

	l.Title = strings.TrimSpace(l.Title)
	l.ShortDescription = strings.TrimSpace(l.ShortDescription)
	l.FullDescription = strings.TrimSpace(l.FullDescription)
	l.Video = strings.TrimSpace(l.Video)

	dir := filepath.Join(w.target, l.Language)
	if err := validateListing(l, dir); err != nil {
		return err
	}

	files := []struct {
		name  string
		value string
	}{
		{"title.txt", l.Title},
		{"short_description.txt", l.ShortDescription},
		{"full_description.txt", l.FullDescription},
	}
	for _, file := range files {
		if err := writeTextFile(filepath.Join(w.stage, l.Language, file.name), file.value); err != nil {
			return err
		}
	}
	if l.Video != "" {
		if err := writeTextFile(filepath.Join(w.stage, l.Language, "video.txt"), l.Video); err != nil {
			return err
		}
	}

	w.listings[l.Language] = true
	return nil
}

// AddImage writes one store image. Singular types keep their conventional file
// name; screenshots are numbered in the order Play returned them, which is the
// order Play displays.
func (w *Writer) AddImage(language, imageType string, content []byte) error {
	if err := release.ValidateLocale(language); err != nil {
		return err
	}
	if len(content) == 0 {
		return exitcode.Errorf(exitcode.Usage, "Play returned an empty %s image for %s", imageType, language)
	}
	if len(content) > MaxImageSize {
		return exitcode.Errorf(exitcode.Usage,
			"%s image for %s is %d bytes, limit is %d", imageType, language, len(content), MaxImageSize)
	}

	_, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return exitcode.Errorf(exitcode.Usage,
			"%s image for %s is not a readable PNG or JPEG: %v", imageType, language, err)
	}
	var ext string
	switch format {
	case "png":
		ext = ".png"
	case "jpeg":
		ext = ".jpg"
	default:
		return exitcode.Errorf(exitcode.Usage,
			"%s image for %s has unsupported format %q: want PNG or JPEG", imageType, language, format)
	}

	key := language + "\x00" + imageType
	if base, ok := imageFileBase(imageType); ok {
		if w.single[key] {
			return exitcode.Errorf(exitcode.Usage,
				"Play returned more than one %s image for %s", imageType, language)
		}
		path := filepath.Join(w.stage, language, "images", base+ext)
		if err := writeImageFile(path, content); err != nil {
			return err
		}
		w.single[key] = true
		return nil
	}
	if !isScreenshotType(imageType) {
		return exitcode.Errorf(exitcode.Usage, "unknown image type %q", imageType)
	}

	w.screenshots[key]++
	path := filepath.Join(w.stage, language, "images", imageType,
		fmt.Sprintf("%03d%s", w.screenshots[key], ext))
	if err := writeImageFile(path, content); err != nil {
		return err
	}
	return nil
}

// AddTesters writes the Google Groups of one track.
func (w *Writer) AddTesters(t Testers) error {
	if err := release.ValidateTrackName(t.Track); err != nil {
		return err
	}
	if w.testers[t.Track] {
		return exitcode.Errorf(exitcode.Usage, "Play returned testers for %s more than once", t.Track)
	}
	if len(t.GoogleGroups) == 0 {
		return exitcode.Errorf(exitcode.Usage, "Play returned no tester groups for %s", t.Track)
	}

	path := filepath.Join(w.target, "testers", t.Track+".txt")
	groups, err := parseGroups(strings.Join(t.GoogleGroups, "\n"), path)
	if err != nil {
		return err
	}
	if err := writeTextFile(filepath.Join(w.stage, "testers", t.Track+".txt"), strings.Join(groups, "\n")); err != nil {
		return err
	}
	w.testers[t.Track] = true
	return nil
}

// AddChangelog writes release notes for one locale and version code. Empty
// notes are skipped: Play returns an empty text when a locale has no notes.
func (w *Writer) AddChangelog(language string, versionCode int64, text string) error {
	if err := release.ValidateLocale(language); err != nil {
		return err
	}
	if versionCode <= 0 {
		return exitcode.Errorf(exitcode.Usage, "invalid version code %d for release notes", versionCode)
	}

	body := strings.TrimSpace(text)
	if body == "" {
		return nil
	}
	if length := utf8.RuneCountInString(body); length > release.MaxNoteLength {
		return exitcode.Errorf(exitcode.Usage,
			"release notes for %s and version code %d have %d characters, limit is %d",
			language, versionCode, length, release.MaxNoteLength)
	}

	key := fmt.Sprintf("%s\x00%d", language, versionCode)
	if w.changelogs[key] {
		return exitcode.Errorf(exitcode.Usage,
			"Play returned release notes for %s and version code %d more than once", language, versionCode)
	}
	path := filepath.Join(w.stage, language, "changelogs", fmt.Sprintf("%d.txt", versionCode))
	if err := writeTextFile(path, body); err != nil {
		return err
	}
	w.changelogs[key] = true
	return nil
}

func imageFileBase(imageType string) (string, bool) {
	for name, mapped := range singleImages {
		if mapped == imageType {
			return name, true
		}
	}
	return "", false
}

func isScreenshotType(imageType string) bool {
	return containsString(screenshotDirs, imageType)
}

func writeTextFile(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the metadata directory permissions",
		)
	}
	data := []byte(body)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the metadata directory permissions",
		)
	}
	return nil
}

func writeImageFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the metadata directory permissions",
		)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the metadata directory permissions",
		)
	}
	return nil
}
