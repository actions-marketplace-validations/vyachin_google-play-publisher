package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/release"
)

// readChangelogDir reads release notes for versionCode from a
// Play-style metadata directory: <dir>/<locale>/changelogs/<versionCode>.txt.
// A metadata directory may contain only changelogs, so missing notes are not
// an error; locales without a file for that version code are skipped.
func readChangelogDir(dir string, versionCode int64) (map[string]string, error) {
	if dir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the path passed to --metadata-dir",
		)
	}

	name := strconv.FormatInt(versionCode, 10) + ".txt"
	notes := make(map[string]string)

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		locale := entry.Name()
		path := filepath.Join(dir, locale, "changelogs", name)

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

	if len(notes) == 0 {
		return nil, nil
	}
	return notes, nil
}
