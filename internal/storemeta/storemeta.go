// Package storemeta reads a Play-style metadata directory: localized store
// listings, store images and tester groups that are applied together with a
// release in the same edit.
package storemeta

import (
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register the JPEG decoder for image.DecodeConfig
	_ "image/png"  // register the PNG decoder for image.DecodeConfig
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/release"
)

// Play limits for listing text, in characters.
const (
	MaxTitleLength      = 30
	MaxShortDescription = 80
	MaxFullDescription  = 4000
)

// MaxImageSize bounds one image file. Play rejects larger screenshots; the
// check catches an accidentally committed source asset before the upload.
const MaxImageSize = 8 << 20

// Play image types. The API calls the app icon simply "icon"; the metadata
// tree stores it as images/icon.png.
const (
	ImageAppIcon        = "icon"
	ImageFeatureGraphic = "featureGraphic"
	ImageTVBanner       = "tvBanner"
)

// ImageTypes returns the image types in Play order: singular images first,
// then galleries. Pull and publish use the same list, so a pulled tree is
// always a complete description of what Play exposes.
func ImageTypes() []string {
	types := []string{ImageAppIcon, ImageFeatureGraphic, ImageTVBanner}
	return append(types, screenshotDirs...)
}

// Required listing files. A locale that has any listing file must have all
// three, so a partial directory cannot wipe a description.
var listingFiles = []string{"title.txt", "short_description.txt", "full_description.txt"}

// singleImages maps a metadata file name to a Play image type.
var singleImages = map[string]string{
	"icon":           ImageAppIcon,
	"featureGraphic": ImageFeatureGraphic,
	"tvBanner":       ImageTVBanner,
}

// screenshotDirs are Play image types stored as a directory of images.
var screenshotDirs = []string{
	"phoneScreenshots",
	"sevenInchScreenshots",
	"tenInchScreenshots",
	"tvScreenshots",
	"wearScreenshots",
}

var imageExtensions = map[string]bool{".png": true, ".jpg": true, ".jpeg": true}

// Listing is the localized store listing text for one language.
type Listing struct {
	Language         string
	Title            string
	ShortDescription string
	FullDescription  string
	Video            string
}

// ImageSet is one image type for one language. An empty Files slice removes
// every image of that type: the directory is authoritative for the types it
// mentions.
type ImageSet struct {
	Language string
	Type     string
	Files    []string
}

// Testers lists the Google Groups of one track.
type Testers struct {
	Track        string
	GoogleGroups []string
}

// Metadata is a validated metadata directory.
type Metadata struct {
	Listings []Listing
	Images   []ImageSet
	Testers  []Testers
}

// Empty reports whether nothing would be applied.
func (m *Metadata) Empty() bool {
	return len(m.Listings) == 0 && len(m.Images) == 0 && len(m.Testers) == 0
}

// Read parses and validates a metadata directory. An empty path, or a
// directory that only contains changelogs, returns nil so callers can treat
// "no store metadata" uniformly.
//
// Layout:
//
//	<dir>/<locale>/title.txt
//	<dir>/<locale>/short_description.txt
//	<dir>/<locale>/full_description.txt
//	<dir>/<locale>/video.txt
//	<dir>/<locale>/images/icon.png
//	<dir>/<locale>/images/featureGraphic.png
//	<dir>/<locale>/images/<type>/<image files>
//	<dir>/testers/<track>.txt
func Read(dir string) (*Metadata, error) {
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

	meta := &Metadata{}
	locales := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case strings.HasPrefix(name, "."), !entry.IsDir():
			continue
		case name == "testers":
			continue
		}
		locales = append(locales, name)
	}
	sort.Strings(locales)

	for _, locale := range locales {
		localeDir := filepath.Join(dir, locale)

		listing, ok, err := readListing(locale, localeDir)
		if err != nil {
			return nil, err
		}
		if ok {
			meta.Listings = append(meta.Listings, listing)
		}

		images, err := readImages(locale, localeDir)
		if err != nil {
			return nil, err
		}
		meta.Images = append(meta.Images, images...)
	}

	testers, err := readTesters(filepath.Join(dir, "testers"))
	if err != nil {
		return nil, err
	}
	meta.Testers = testers

	if meta.Empty() {
		return nil, nil
	}

	return meta, nil
}

// readListing reads one localized listing. A locale without any listing file
// is skipped: it may only contribute changelogs or images.
func readListing(locale, dir string) (Listing, bool, error) {
	present := make(map[string]string, len(listingFiles)+1)
	for _, name := range append(append([]string{}, listingFiles...), "video.txt") {
		path := filepath.Join(dir, name)

		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			present[name] = string(data)
		case errors.Is(err, fs.ErrNotExist):
		default:
			return Listing{}, false, exitcode.WithHint(
				exitcode.Wrap(exitcode.Usage, err),
				"check the metadata file permissions",
			)
		}
	}
	if len(present) == 0 {
		return Listing{}, false, nil
	}

	if err := release.ValidateLocale(locale); err != nil {
		return Listing{}, false, err
	}
	for _, name := range listingFiles {
		if _, ok := present[name]; !ok {
			return Listing{}, false, exitcode.Errorf(exitcode.Usage,
				"incomplete listing in %s: %s is missing (a listing needs %s)",
				dir, name, strings.Join(listingFiles, ", "))
		}
	}

	listing := Listing{
		Language:         locale,
		Title:            strings.TrimSpace(present["title.txt"]),
		ShortDescription: strings.TrimSpace(present["short_description.txt"]),
		FullDescription:  strings.TrimSpace(present["full_description.txt"]),
		Video:            strings.TrimSpace(present["video.txt"]),
	}
	if err := validateListing(listing, dir); err != nil {
		return Listing{}, false, err
	}
	return listing, true, nil
}

func validateListing(listing Listing, dir string) error {
	limits := []struct {
		name     string
		value    string
		max      int
		fileName string
	}{
		{"title", listing.Title, MaxTitleLength, "title.txt"},
		{"short description", listing.ShortDescription, MaxShortDescription, "short_description.txt"},
		{"full description", listing.FullDescription, MaxFullDescription, "full_description.txt"},
	}

	for _, limit := range limits {
		if limit.value == "" {
			return exitcode.Errorf(exitcode.Usage,
				"listing %s is empty in %s", filepath.Join(dir, limit.fileName), dir)
		}
		if length := utf8.RuneCountInString(limit.value); length > limit.max {
			return exitcode.Errorf(exitcode.Usage,
				"listing %s in %s has %d characters, limit is %d",
				limit.fileName, dir, length, limit.max)
		}
	}

	if listing.Video != "" &&
		!strings.HasPrefix(listing.Video, "https://") && !strings.HasPrefix(listing.Video, "http://") {
		return exitcode.Errorf(exitcode.Usage,
			"video in %s is not an http(s) URL: %q", dir, listing.Video)
	}
	return nil
}

// readImages reads every image set of one locale.
func readImages(locale, dir string) ([]ImageSet, error) {
	imagesDir := filepath.Join(dir, "images")

	info, err := os.Stat(imagesDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, exitcode.WithHint(exitcode.Wrap(exitcode.Usage, err), "check the images directory")
	case !info.IsDir():
		return nil, exitcode.Errorf(exitcode.Usage, "%s is not a directory", imagesDir)
	}

	entries, err := os.ReadDir(imagesDir)
	if err != nil {
		return nil, exitcode.WithHint(exitcode.Wrap(exitcode.Usage, err), "check the images directory")
	}

	if err := release.ValidateLocale(locale); err != nil {
		return nil, err
	}

	sets := make([]ImageSet, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		if entry.IsDir() {
			if !containsString(screenshotDirs, name) {
				return nil, exitcode.Errorf(exitcode.Usage,
					"unknown image directory %s: want one of %s",
					filepath.Join(imagesDir, name), strings.Join(screenshotDirs, ", "))
			}
			files, err := readScreenshots(filepath.Join(imagesDir, name), name)
			if err != nil {
				return nil, err
			}
			sets = append(sets, ImageSet{Language: locale, Type: name, Files: files})
			continue
		}

		base := strings.TrimSuffix(name, filepath.Ext(name))
		imageType, ok := singleImages[base]
		if !ok {
			return nil, exitcode.Errorf(exitcode.Usage,
				"unknown image file %s: want %s with a .png, .jpg or .jpeg extension",
				filepath.Join(imagesDir, name), strings.Join(sortedKeys(singleImages), ", "))
		}
		path := filepath.Join(imagesDir, name)
		if err := validateImage(path, imageType); err != nil {
			return nil, err
		}
		sets = append(sets, ImageSet{Language: locale, Type: imageType, Files: []string{path}})
	}

	return sets, nil
}

// readScreenshots reads a screenshot directory. An empty directory is valid
// and clears every image of that type.
func readScreenshots(dir, imageType string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, exitcode.WithHint(exitcode.Wrap(exitcode.Usage, err), "check the screenshots directory")
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsDir() {
			return nil, exitcode.Errorf(exitcode.Usage,
				"unexpected directory %s inside %s", name, dir)
		}
		if !imageExtensions[strings.ToLower(filepath.Ext(name))] {
			return nil, exitcode.Errorf(exitcode.Usage,
				"unsupported image %s: want .png, .jpg or .jpeg", filepath.Join(dir, name))
		}
		path := filepath.Join(dir, name)
		if err := validateImage(path, imageType); err != nil {
			return nil, err
		}
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
}

// validateImage checks size, format and the two Play images with fixed
// dimensions.
func validateImage(path, imageType string) error {
	if !imageExtensions[strings.ToLower(filepath.Ext(path))] {
		return exitcode.Errorf(exitcode.Usage,
			"unsupported image %s: want .png, .jpg or .jpeg", path)
	}

	info, err := os.Stat(path)
	if err != nil {
		return exitcode.WithHint(exitcode.Wrap(exitcode.Usage, err), "check the image path")
	}
	if info.Size() == 0 {
		return exitcode.Errorf(exitcode.Usage, "image %s is empty", path)
	}
	if info.Size() > MaxImageSize {
		return exitcode.Errorf(exitcode.Usage,
			"image %s is %d bytes, limit is %d", path, info.Size(), MaxImageSize)
	}

	file, err := os.Open(path)
	if err != nil {
		return exitcode.WithHint(exitcode.Wrap(exitcode.Usage, err), "check the image path")
	}
	defer func() { _ = file.Close() }()

	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return exitcode.Errorf(exitcode.Usage, "image %s is not a readable PNG or JPEG: %v", path, err)
	}

	switch imageType {
	case ImageAppIcon:
		if config.Width != 512 || config.Height != 512 {
			return exitcode.Errorf(exitcode.Usage,
				"app icon %s is %dx%d, Play requires 512x512", path, config.Width, config.Height)
		}
	case ImageFeatureGraphic:
		if config.Width != 1024 || config.Height != 500 {
			return exitcode.Errorf(exitcode.Usage,
				"feature graphic %s is %dx%d, Play requires 1024x500", path, config.Width, config.Height)
		}
	}
	return nil
}

// readTesters reads <dir>/<track>.txt files with one Google Group per line.
func readTesters(dir string) ([]Testers, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, exitcode.WithHint(exitcode.Wrap(exitcode.Usage, err), "check the testers directory")
	}

	testers := make([]Testers, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".txt") {
			return nil, exitcode.Errorf(exitcode.Usage,
				"unexpected entry %s in %s: want <track>.txt", filepath.Join(dir, name), dir)
		}

		track := strings.TrimSuffix(name, ".txt")
		if err := release.ValidateTrackName(track); err != nil {
			return nil, err
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, exitcode.WithHint(exitcode.Wrap(exitcode.Usage, err), "check the tester file")
		}

		groups, err := parseGroups(string(data), filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		testers = append(testers, Testers{Track: track, GoogleGroups: groups})
	}

	sort.Slice(testers, func(i, j int) bool { return testers[i].Track < testers[j].Track })
	return testers, nil
}

func parseGroups(body, path string) ([]string, error) {
	groups := make([]string, 0)
	seen := make(map[string]bool)
	for lineNumber, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "@") || strings.ContainsAny(line, " \t,") {
			return nil, exitcode.Errorf(exitcode.Usage,
				"tester %q on line %d of %s is not an email address", line, lineNumber+1, path)
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		groups = append(groups, line)
	}

	if len(groups) == 0 {
		return nil, exitcode.Errorf(exitcode.Usage, "tester file %s lists no Google Groups", path)
	}
	sort.Strings(groups)
	return groups, nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// String renders a listing for error messages and tests.
func (l Listing) String() string {
	return fmt.Sprintf("%s (%q)", l.Language, l.Title)
}
