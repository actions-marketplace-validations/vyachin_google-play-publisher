// Package playtest provides a fake play.API for tests.
package playtest

import (
	"context"
	"io"
	"path/filepath"

	"github.com/vyachin/google-play-publisher/internal/play"
)

// Fake is a recording play.API implementation. Unset functions return
// sensible defaults, so tests only configure what they exercise.
type Fake struct {
	CreateEditFunc      func(ctx context.Context, pkg string) (string, error)
	DeleteEditFunc      func(ctx context.Context, pkg, editID string) error
	UploadBundleFunc    func(ctx context.Context, pkg, editID string, r io.Reader, size int64) (play.BundleInfo, error)
	UpdateTrackFunc     func(ctx context.Context, pkg, editID, track string, rel play.Release) error
	TrackEditFunc       func(ctx context.Context, pkg, editID, track string) (play.TrackState, error)
	UpdateListingFunc   func(ctx context.Context, pkg, editID string, listing play.Listing) error
	StoreListingsFunc   func(ctx context.Context, pkg, editID string) ([]play.Listing, error)
	DeleteAllImagesFunc func(ctx context.Context, pkg, editID, language, imageType string) error
	UploadImageFunc     func(ctx context.Context, pkg, editID, language, imageType, path string) error
	StoreImagesFunc     func(ctx context.Context, pkg, editID, language, imageType string) ([]play.Image, error)
	DownloadImageFunc   func(ctx context.Context, url string) ([]byte, error)
	UpdateTestersFunc   func(ctx context.Context, pkg, editID string, testers play.Testers) error
	TesterGroupsFunc    func(ctx context.Context, pkg, editID, track string) (play.Testers, error)
	AllTracksFunc       func(ctx context.Context, pkg, editID string) ([]play.TrackState, error)
	ValidateEditFunc    func(ctx context.Context, pkg, editID string) error
	CommitEditFunc      func(ctx context.Context, pkg, editID string, opts play.CommitOptions) error
	TrackReleasesFunc   func(ctx context.Context, pkg, track string) ([]play.ReleaseSummary, error)

	Calls              []string
	DeletedEditIDs     []string
	CommitOptions      []play.CommitOptions
	UploadedSizes      []int64
	UpdatedReleases    []play.Release
	UpdatedTracks      []string
	ReadTracks         []string
	Listings           []play.Listing
	ClearedImages      []string
	UploadedImages     []string
	TesterUpdates      []play.Testers
	DownloadedImageURL []string
	ReadTesterTracks   []string
}

// CreateEdit records the call and returns "edit-1" by default.
func (f *Fake) CreateEdit(ctx context.Context, pkg string) (string, error) {
	f.Calls = append(f.Calls, "CreateEdit")
	if f.CreateEditFunc != nil {
		return f.CreateEditFunc(ctx, pkg)
	}
	return "edit-1", nil
}

// DeleteEdit records the call.
func (f *Fake) DeleteEdit(ctx context.Context, pkg, editID string) error {
	f.Calls = append(f.Calls, "DeleteEdit")
	f.DeletedEditIDs = append(f.DeletedEditIDs, editID)
	if f.DeleteEditFunc != nil {
		return f.DeleteEditFunc(ctx, pkg, editID)
	}
	return nil
}

// UploadBundle records the call and returns version code 42 by default.
func (f *Fake) UploadBundle(ctx context.Context, pkg, editID string, r io.Reader, size int64) (play.BundleInfo, error) {
	f.Calls = append(f.Calls, "UploadBundle")
	f.UploadedSizes = append(f.UploadedSizes, size)
	if f.UploadBundleFunc != nil {
		return f.UploadBundleFunc(ctx, pkg, editID, r, size)
	}
	return play.BundleInfo{VersionCode: 42}, nil
}

// UpdateTrack records the call.
func (f *Fake) UpdateTrack(ctx context.Context, pkg, editID, track string, rel play.Release) error {
	f.Calls = append(f.Calls, "UpdateTrack")
	f.UpdatedReleases = append(f.UpdatedReleases, rel)
	f.UpdatedTracks = append(f.UpdatedTracks, track)
	if f.UpdateTrackFunc != nil {
		return f.UpdateTrackFunc(ctx, pkg, editID, track, rel)
	}
	return nil
}

// TrackEdit records the call and returns an empty track by default.
func (f *Fake) TrackEdit(ctx context.Context, pkg, editID, track string) (play.TrackState, error) {
	f.Calls = append(f.Calls, "TrackEdit")
	f.ReadTracks = append(f.ReadTracks, track)
	if f.TrackEditFunc != nil {
		return f.TrackEditFunc(ctx, pkg, editID, track)
	}
	return play.TrackState{Track: track}, nil
}

// UpdateListing records the call.
func (f *Fake) UpdateListing(ctx context.Context, pkg, editID string, listing play.Listing) error {
	f.Calls = append(f.Calls, "UpdateListing")
	f.Listings = append(f.Listings, listing)
	if f.UpdateListingFunc != nil {
		return f.UpdateListingFunc(ctx, pkg, editID, listing)
	}
	return nil
}

// StoreListings records the call and returns no listings by default.
func (f *Fake) StoreListings(ctx context.Context, pkg, editID string) ([]play.Listing, error) {
	f.Calls = append(f.Calls, "StoreListings")
	if f.StoreListingsFunc != nil {
		return f.StoreListingsFunc(ctx, pkg, editID)
	}
	return nil, nil
}

// StoreImages records the call as "language/type" and returns no images by default.
func (f *Fake) StoreImages(ctx context.Context, pkg, editID, language, imageType string) ([]play.Image, error) {
	f.Calls = append(f.Calls, "StoreImages")
	if f.StoreImagesFunc != nil {
		return f.StoreImagesFunc(ctx, pkg, editID, language, imageType)
	}
	return nil, nil
}

// DownloadImage records the URL and returns no bytes by default.
func (f *Fake) DownloadImage(ctx context.Context, url string) ([]byte, error) {
	f.Calls = append(f.Calls, "DownloadImage")
	f.DownloadedImageURL = append(f.DownloadedImageURL, url)
	if f.DownloadImageFunc != nil {
		return f.DownloadImageFunc(ctx, url)
	}
	return nil, nil
}

// TesterGroups records the track and returns empty testers by default.
func (f *Fake) TesterGroups(ctx context.Context, pkg, editID, track string) (play.Testers, error) {
	f.Calls = append(f.Calls, "TesterGroups")
	f.ReadTesterTracks = append(f.ReadTesterTracks, track)
	if f.TesterGroupsFunc != nil {
		return f.TesterGroupsFunc(ctx, pkg, editID, track)
	}
	return play.Testers{Track: track}, nil
}

// AllTracks records the call and returns no tracks by default.
func (f *Fake) AllTracks(ctx context.Context, pkg, editID string) ([]play.TrackState, error) {
	f.Calls = append(f.Calls, "AllTracks")
	if f.AllTracksFunc != nil {
		return f.AllTracksFunc(ctx, pkg, editID)
	}
	return nil, nil
}

// DeleteAllImages records the call as "language/type".
func (f *Fake) DeleteAllImages(ctx context.Context, pkg, editID, language, imageType string) error {
	f.Calls = append(f.Calls, "DeleteAllImages")
	f.ClearedImages = append(f.ClearedImages, language+"/"+imageType)
	if f.DeleteAllImagesFunc != nil {
		return f.DeleteAllImagesFunc(ctx, pkg, editID, language, imageType)
	}
	return nil
}

// UploadImage records the call as "language/type/path".
func (f *Fake) UploadImage(ctx context.Context, pkg, editID, language, imageType, path string) error {
	f.Calls = append(f.Calls, "UploadImage")
	f.UploadedImages = append(f.UploadedImages, language+"/"+imageType+"/"+filepath.Base(path))
	if f.UploadImageFunc != nil {
		return f.UploadImageFunc(ctx, pkg, editID, language, imageType, path)
	}
	return nil
}

// UpdateTesters records the call.
func (f *Fake) UpdateTesters(ctx context.Context, pkg, editID string, testers play.Testers) error {
	f.Calls = append(f.Calls, "UpdateTesters")
	f.TesterUpdates = append(f.TesterUpdates, testers)
	if f.UpdateTestersFunc != nil {
		return f.UpdateTestersFunc(ctx, pkg, editID, testers)
	}
	return nil
}

// ValidateEdit records the call.
func (f *Fake) ValidateEdit(ctx context.Context, pkg, editID string) error {
	f.Calls = append(f.Calls, "ValidateEdit")
	if f.ValidateEditFunc != nil {
		return f.ValidateEditFunc(ctx, pkg, editID)
	}
	return nil
}

// CommitEdit records the call and options.
func (f *Fake) CommitEdit(ctx context.Context, pkg, editID string, opts play.CommitOptions) error {
	f.Calls = append(f.Calls, "CommitEdit")
	f.CommitOptions = append(f.CommitOptions, opts)
	if f.CommitEditFunc != nil {
		return f.CommitEditFunc(ctx, pkg, editID, opts)
	}
	return nil
}

// TrackReleases records the call and returns no releases by default.
func (f *Fake) TrackReleases(ctx context.Context, pkg, track string) ([]play.ReleaseSummary, error) {
	f.Calls = append(f.Calls, "TrackReleases")
	if f.TrackReleasesFunc != nil {
		return f.TrackReleasesFunc(ctx, pkg, track)
	}
	return nil, nil
}
