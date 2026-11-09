package app

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/release"
)

func writeChangelogFile(t *testing.T, root, locale string, versionCode int64, text string) {
	t.Helper()

	dir := filepath.Join(root, locale, "changelogs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create changelog dir: %v", err)
	}
	path := filepath.Join(dir, strconv.FormatInt(versionCode, 10)+".txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("write changelog: %v", err)
	}
}

func TestReadChangelogDirEmptyPath(t *testing.T) {
	notes, err := readChangelogDir("", 42)

	if err != nil || notes != nil {
		t.Fatalf("notes = %#v, err = %v; want nil, nil", notes, err)
	}
}

func TestReadChangelogDirSkipsLocalesWithoutFile(t *testing.T) {
	root := t.TempDir()
	writeChangelogFile(t, root, "ru-RU", 42, "Исправления")
	writeChangelogFile(t, root, "en-US", 41, "Old release")

	notes, err := readChangelogDir(root, 42)
	if err != nil {
		t.Fatalf("read changelog: %v", err)
	}
	if len(notes) != 1 || notes["ru-RU"] != "Исправления" {
		t.Fatalf("notes = %#v, want only ru-RU", notes)
	}
}

func TestReadChangelogDirMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "ru-RU", "changelogs"), 0o755); err != nil {
		t.Fatalf("create changelog dir: %v", err)
	}

	_, err := readChangelogDir(root, 42)

	if exitcode.From(err) != exitcode.Usage {
		t.Fatalf("code = %d, want %d (err: %v)", exitcode.From(err), exitcode.Usage, err)
	}
	if !strings.Contains(err.Error(), "no changelog for versionCode 42") {
		t.Fatalf("unexpected error: %v", err)
	}

	var coded *exitcode.Error
	if !errors.As(err, &coded) || !strings.Contains(coded.Hint, "42.txt") {
		t.Fatalf("hint does not name the expected file: %v", err)
	}
}

func TestReadChangelogDirEmptyFile(t *testing.T) {
	root := t.TempDir()
	writeChangelogFile(t, root, "ru-RU", 42, "   \n")

	_, err := readChangelogDir(root, 42)

	if exitcode.From(err) != exitcode.Usage {
		t.Fatalf("code = %d, want %d (err: %v)", exitcode.From(err), exitcode.Usage, err)
	}
	if !strings.Contains(err.Error(), "is empty") || !strings.Contains(err.Error(), "42.txt") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadChangelogDirTooLong(t *testing.T) {
	root := t.TempDir()
	writeChangelogFile(t, root, "ru-RU", 42, strings.Repeat("я", release.MaxNoteLength+1))

	_, err := readChangelogDir(root, 42)

	if exitcode.From(err) != exitcode.Usage {
		t.Fatalf("code = %d, want %d (err: %v)", exitcode.From(err), exitcode.Usage, err)
	}
	if !strings.Contains(err.Error(), "limit is") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadChangelogDirNotADirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := readChangelogDir(path, 42)

	if exitcode.From(err) != exitcode.Usage {
		t.Fatalf("code = %d, want %d (err: %v)", exitcode.From(err), exitcode.Usage, err)
	}
}
