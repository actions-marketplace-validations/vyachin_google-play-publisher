package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
)

func writeMetadataTree(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	files := map[string]string{
		"en-US/title.txt":             "Example",
		"en-US/short_description.txt": "Short",
		"en-US/full_description.txt":  "Full description",
		"testers/internal.txt":        "qa@googlegroups.com\n",
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return dir
}

func TestPublishMetadataDirJSON(t *testing.T) {
	fake := &playtest.Fake{}

	out, errOut, code := runWithAPI(t, fake, "publish", "-f", writeAAB(t),
		"--metadata-dir", writeMetadataTree(t), "--json")
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want 0: %s", code, errOut)
	}

	got := decodeSingleObject(t, out)
	metadata, ok := got["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata = %#v, want an object", got["metadata"])
	}
	if metadata["listings"] != float64(1) || metadata["testers"] != float64(1) {
		t.Fatalf("metadata = %v", metadata)
	}
	if len(fake.Listings) != 1 || fake.Listings[0].Language != "en-US" {
		t.Fatalf("listings = %+v", fake.Listings)
	}
}

func TestPublishMetadataDirTextOutput(t *testing.T) {
	fake := &playtest.Fake{}

	out, errOut, code := runWithAPI(t, fake, "publish", "-f", writeAAB(t),
		"--metadata-dir", writeMetadataTree(t))
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want 0: %s", code, errOut)
	}
	if !strings.Contains(out, "[metadata: 1 listings, 0 images, 1 tester lists]") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestPublishRejectsMetadataDirWithChangelogDir(t *testing.T) {
	fake := &playtest.Fake{}

	_, errOut, code := runWithAPI(t, fake, "publish", "-f", writeAAB(t),
		"--metadata-dir", writeMetadataTree(t), "--changelog-dir", t.TempDir())
	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d: %s", code, exitcode.Usage, errOut)
	}
	if !strings.Contains(errOut, "cannot be combined") {
		t.Fatalf("unexpected error: %s", errOut)
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("API was called for invalid input: %v", fake.Calls)
	}
}
