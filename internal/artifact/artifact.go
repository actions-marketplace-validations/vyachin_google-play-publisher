// Package artifact inspects Android App Bundle files and AGP build metadata.
package artifact

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

// Info describes an inspected Android App Bundle.
type Info struct {
	Path   string
	Size   int64
	SHA256 string
}

// Inspect verifies that path is a readable Android App Bundle and returns its
// size and SHA-256 checksum.
func Inspect(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the path passed to --aab",
		)
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return Info{}, exitcode.Wrap(exitcode.Usage, err)
	}
	if stat.Size() == 0 {
		return Info{}, exitcode.Errorf(exitcode.Usage, "%s is empty", path)
	}

	reader, err := zip.NewReader(f, stat.Size())
	if err != nil {
		return Info{}, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, fmt.Errorf("%s is not a ZIP archive: %w", path, err)),
			"pass a signed .aab file produced by Gradle or bundletool",
		)
	}

	if !hasBundleConfig(reader) {
		return Info{}, exitcode.WithHint(
			exitcode.Errorf(exitcode.Usage, "%s is not an Android App Bundle: BundleConfig.pb not found", path),
			"APK files are not supported; pass a .aab file",
		)
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Info{}, exitcode.Wrap(exitcode.Usage, err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return Info{}, exitcode.Wrap(exitcode.Usage, err)
	}

	return Info{
		Path:   path,
		Size:   stat.Size(),
		SHA256: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func hasBundleConfig(reader *zip.Reader) bool {
	for _, file := range reader.File {
		if file.Name == "BundleConfig.pb" {
			return true
		}
	}
	return false
}
