// Package artifact inspects Android App Bundle files and AGP build metadata.
package artifact

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

// Info describes an inspected Android App Bundle.
type Info struct {
	Path   string
	Size   int64
	SHA256 string
	Signed bool
}

// Bundle is an opened AAB together with its inspection result and the release
// identity parsed from the base manifest. The caller owns File until Close.
type Bundle struct {
	File     *os.File
	Info     Info
	Identity Identity
}

// Open opens and inspects an AAB and parses its base manifest. The returned
// file is rewound to the start and is closed by Bundle.Close.
func Open(path string) (*Bundle, error) {
	file, size, reader, err := openBundle(path)
	if err != nil {
		return nil, err
	}

	identity, err := readIdentity(reader, path)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	info, err := inspectOpened(file, path, size, reader)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	return &Bundle{File: file, Info: info, Identity: identity}, nil
}

// Close closes the underlying file. It is safe to call on a zero Bundle and
// more than once.
func (b *Bundle) Close() error {
	if b == nil || b.File == nil {
		return nil
	}
	file := b.File
	b.File = nil
	return file.Close()
}

// Rewind moves the file offset back to the start before an upload.
func (b *Bundle) Rewind() error {
	if b == nil || b.File == nil {
		return exitcode.Errorf(exitcode.Internal, "artifact bundle is not open")
	}
	_, err := b.File.Seek(0, io.SeekStart)
	return err
}

// Inspect verifies that path is a readable Android App Bundle and returns its
// size and SHA-256 checksum. It does not parse the manifest; use Open when the
// release identity is needed.
func Inspect(path string) (Info, error) {
	file, size, reader, err := openBundle(path)
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = file.Close() }()

	return inspectOpened(file, path, size, reader)
}

func openBundle(path string) (*os.File, int64, *zip.Reader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the path passed to --aab",
		)
	}

	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, nil, exitcode.Wrap(exitcode.Usage, err)
	}
	if stat.Size() == 0 {
		_ = file.Close()
		return nil, 0, nil, exitcode.Errorf(exitcode.Usage, "%s is empty", path)
	}

	reader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		_ = file.Close()
		return nil, 0, nil, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"pass a signed .aab file produced by Gradle or bundletool",
		)
	}

	if !hasBundleConfig(reader) {
		_ = file.Close()
		return nil, 0, nil, exitcode.WithHint(
			exitcode.Errorf(exitcode.Usage, "%s is not an Android App Bundle: BundleConfig.pb not found", path),
			"APK files are not supported; pass a .aab file",
		)
	}

	return file, stat.Size(), reader, nil
}

func inspectOpened(file *os.File, path string, size int64, reader *zip.Reader) (Info, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Info{}, exitcode.Wrap(exitcode.Usage, err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return Info{}, exitcode.Wrap(exitcode.Usage, err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Info{}, exitcode.Wrap(exitcode.Usage, err)
	}

	return Info{
		Path:   path,
		Size:   size,
		SHA256: hex.EncodeToString(hash.Sum(nil)),
		Signed: hasSignature(reader),
	}, nil
}

// hasSignature reports whether the bundle contains a JAR signature, which is
// how Gradle and bundletool sign AAB files.
func hasSignature(reader *zip.Reader) bool {
	for _, file := range reader.File {
		name := strings.ToLower(file.Name)
		if !strings.HasPrefix(name, "meta-inf/") {
			continue
		}
		if strings.HasSuffix(name, ".rsa") || strings.HasSuffix(name, ".ec") || strings.HasSuffix(name, ".dsa") {
			return true
		}
	}
	return false
}

func hasBundleConfig(reader *zip.Reader) bool {
	for _, file := range reader.File {
		if file.Name == "BundleConfig.pb" {
			return true
		}
	}
	return false
}
