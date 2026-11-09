package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/release"
)

// readChangelogDir reads release notes for versionCode from a Play-style
// metadata directory: <dir>/<locale>/changelogs/<versionCode>.txt.
//
// Locales without a file for that version code are skipped, so a partial
// translation is allowed. When no locale has a file, the error names the
// paths that were checked.
func readChangelogDir(dir string, versionCode int64) (map[string]string, error) {
	return readChangelogs(dir, versionCode, true)
}

// readChangelogDirOptional is the metadata-directory variant: changelogs are
// optional there, because the same directory may only carry listings or images.
func readChangelogDirOptional(dir string, versionCode int64) (map[string]string, error) {
	return readChangelogs(dir, versionCode, false)
}

func readChangelogs(dir string, versionCode int64, required bool) (map[string]string, error) {
	if dir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the path passed to --changelog-dir",
		)
	}

	name := strconv.FormatInt(versionCode, 10) + ".txt"
	notes := make(map[string]string)
	var checked []string

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		locale := entry.Name()
		path := filepath.Join(dir, locale, "changelogs", name)
		checked = append(checked, path)

		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			text := string(data)
			if strings.TrimSpace(text) == "" {
				return nil, exitcode.Errorf(exitcode.Usage, "changelog %s is empty", path)
			}
			if length := utf8.RuneCountInString(text); length > release.MaxNoteLength {
				return nil, exitcode.Errorf(exitcode.Usage,
					"changelog %s has %d characters, limit is %d", path, length, release.MaxNoteLength)
			}
			notes[locale] = text
		case errors.Is(err, fs.ErrNotExist):
			continue
		default:
			return nil, exitcode.WithHint(
				exitcode.Wrap(exitcode.Usage, err),
				"check the changelog path",
			)
		}
	}

	if len(notes) == 0 && !required {
		return nil, nil
	}

	if len(notes) == 0 {
		hint := fmt.Sprintf("add %s", filepath.Join(dir, "<locale>", "changelogs", name))
		if len(checked) > 0 {
			hint = "checked " + strings.Join(checked, ", ")
		}
		return nil, exitcode.WithHint(
			exitcode.Errorf(exitcode.Usage, "no changelog for versionCode %d under %s", versionCode, dir),
			hint,
		)
	}

	return notes, nil
}
