package play

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	"cloud.google.com/go/auth/httptransport"
	"google.golang.org/api/androidpublisher/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

const (
	defaultChunkSize        = 8 << 20 // 8 MiB, a multiple of the required 256 KiB
	defaultChunkRetryWindow = 30 * time.Second
	defaultUserAgent        = "google-play-publisher"
)

// Options configure a Client.
type Options struct {
	// CredentialsFile is a path to a service account JSON key. Empty means ADC.
	CredentialsFile string
	// Endpoint overrides the API base URL. Used by tests.
	Endpoint string
	// UserAgent is sent with every request.
	UserAgent string
	// Logger receives debug and upload progress logs.
	Logger *slog.Logger
	// Debug enables HTTP request/response logging.
	Debug bool
	// HTTPClient replaces the authenticated client. Used by tests.
	HTTPClient *http.Client
	// Retry tunes the bounded retry policy of safely repeatable calls.
	Retry RetryOptions
}

// RetryOptions tune the bounded retry policy. Zero values keep the defaults:
// 5 attempts, 1s base delay and a 32s cap.
type RetryOptions struct {
	Attempts int
	Base     time.Duration
	Cap      time.Duration
}

// Client implements API on top of the official Google API client.
type Client struct {
	svc              *androidpublisher.Service
	logger           *slog.Logger
	chunkSize        int
	chunkRetryWindow time.Duration
	retry            retryConfig
}

// New creates a Play API client. Without Options.HTTPClient it authenticates
// with Application Default Credentials or Options.CredentialsFile.
func New(ctx context.Context, opts Options) (*Client, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	client := opts.HTTPClient
	if client == nil {
		creds, err := credentialsFor(opts.CredentialsFile)
		if err != nil {
			return nil, err
		}
		client, err = authenticatedClient(creds)
		if err != nil {
			return nil, err
		}
	}

	clone := *client
	clone.Transport = newTransport(clone.Transport, transportOptions{
		Logger:    logger,
		Debug:     opts.Debug,
		UserAgent: userAgent(opts.UserAgent),
	})

	svcOpts := []option.ClientOption{option.WithHTTPClient(&clone)}
	if opts.Endpoint != "" {
		svcOpts = append(svcOpts, option.WithEndpoint(strings.TrimRight(opts.Endpoint, "/")+"/"))
	}

	svc, err := androidpublisher.NewService(ctx, svcOpts...)
	if err != nil {
		return nil, exitcode.Wrap(exitcode.Internal, err)
	}

	return &Client{
		svc:              svc,
		logger:           logger,
		chunkSize:        defaultChunkSize,
		chunkRetryWindow: defaultChunkRetryWindow,
		retry:            newRetryConfig(opts.Retry),
	}, nil
}

func newRetryConfig(opts RetryOptions) retryConfig {
	cfg := defaultRetryConfig()
	if opts.Attempts > 0 {
		cfg.attempts = opts.Attempts
	}
	if opts.Base > 0 {
		cfg.base = opts.Base
	}
	if opts.Cap > 0 {
		cfg.cap = opts.Cap
	}
	return cfg
}

// attempts caps the configured attempt count for one operation.
func (c *Client) attempts(limit int) int {
	if limit > 0 && c.retry.attempts > limit {
		return limit
	}
	return c.retry.attempts
}

func userAgent(ua string) string {
	if ua == "" {
		return defaultUserAgent
	}
	return ua
}

func credentialsFor(credentialsFile string) (*auth.Credentials, error) {
	opts := &credentials.DetectOptions{
		Scopes: []string{androidpublisher.AndroidpublisherScope},
	}

	var (
		creds *auth.Credentials
		err   error
	)
	if credentialsFile == "" {
		creds, err = credentials.DetectDefault(opts)
	} else {
		creds, err = credentials.NewCredentialsFromFile(credentials.ServiceAccount, credentialsFile, opts)
	}
	if err != nil {
		return nil, exitcode.WithHint(
			exitcode.Wrap(exitcode.Auth, err),
			"set GOOGLE_APPLICATION_CREDENTIALS to a service account JSON key or pass --credentials",
		)
	}
	return creds, nil
}

// authenticatedClient returns a client whose token source receives the request
// context. oauth2.NewClient cannot do that: the oauth2 TokenSource interface
// has no context and the cloud auth adapter calls the provider with
// context.Background(), so --timeout and cancellation would not cover token
// acquisition or a blocking refresh.
//
// The auth library's own logger stays disabled: at debug level it would print
// token request payloads (JWT assertions or client secrets), which must never
// reach the log. The debug transport already covers the Play API exchanges.
func authenticatedClient(creds *auth.Credentials) (*http.Client, error) {
	client, err := httptransport.NewClient(&httptransport.Options{
		Credentials:      creds,
		DisableTelemetry: true,
	})
	if err != nil {
		return nil, exitcode.Wrap(exitcode.Internal, err)
	}
	return client, nil
}

// CreateEdit creates a new edit and returns its identifier.
func (c *Client) CreateEdit(ctx context.Context, pkg string) (string, error) {
	var edit *androidpublisher.AppEdit

	err := c.call(ctx, "edits.insert", c.attempts(2), func(ctx context.Context) error {
		created, err := c.svc.Edits.Insert(pkg, &androidpublisher.AppEdit{}).Context(ctx).Do()
		if err != nil {
			return err
		}
		edit = created
		return nil
	})
	if err != nil {
		return "", err
	}
	return edit.Id, nil
}

// DeleteEdit discards an edit. It is safe to call for an already deleted edit.
func (c *Client) DeleteEdit(ctx context.Context, pkg, editID string) error {
	err := c.svc.Edits.Delete(pkg, editID).Context(ctx).Do()
	if err == nil {
		return nil
	}

	mapped := mapError(err)
	if exitcode.From(mapped) == exitcode.NotFound {
		return nil
	}
	return mapped
}

// UploadBundle uploads an AAB to an edit and returns its version code and hashes.
func (c *Client) UploadBundle(ctx context.Context, pkg, editID string, r io.Reader, size int64) (BundleInfo, error) {
	call := c.svc.Edits.Bundles.Upload(pkg, editID).Context(ctx)
	// An AAB is a ZIP archive: content sniffing would declare application/zip
	// and Play rejects it with "Media type 'application/zip' is not supported."
	// (HTTP 400). bundles.upload expects application/octet-stream.
	call.Media(r,
		googleapi.ContentType("application/octet-stream"),
		googleapi.ChunkSize(c.chunkSize),
		googleapi.ChunkRetryDeadline(c.chunkRetryWindow),
	)
	call.ProgressUpdater(c.progressUpdater(size))

	bundle, err := call.Do()
	if err != nil {
		return BundleInfo{}, mapError(err)
	}

	return BundleInfo{
		VersionCode: bundle.VersionCode,
		SHA1:        bundle.Sha1,
		SHA256:      bundle.Sha256,
	}, nil
}

// UpdateTrack declares the desired release for a track.
func (c *Client) UpdateTrack(ctx context.Context, pkg, editID, track string, rel Release) error {
	notes := make([]*androidpublisher.LocalizedText, 0, len(rel.ReleaseNotes))
	for _, n := range rel.ReleaseNotes {
		notes = append(notes, &androidpublisher.LocalizedText{Language: n.Language, Text: n.Text})
	}

	update := &androidpublisher.Track{
		Track: track,
		Releases: []*androidpublisher.TrackRelease{{
			VersionCodes:        googleapi.Int64s(rel.VersionCodes),
			Status:              rel.Status,
			UserFraction:        rel.UserFraction,
			ReleaseNotes:        notes,
			Name:                rel.Name,
			InAppUpdatePriority: rel.InAppUpdatePriority,
			CountryTargeting:    countryTargetingToAPI(rel.CountryTargeting),
		}},
	}

	return c.call(ctx, "tracks.update", c.retry.attempts, func(ctx context.Context) error {
		_, err := c.svc.Edits.Tracks.Update(pkg, editID, track, update).Context(ctx).Do()
		return err
	})
}

// TrackEdit reads the current releases of a track inside an edit. It is used
// by rollout and promote, which modify or copy an existing release instead of
// uploading a new bundle.
func (c *Client) TrackEdit(ctx context.Context, pkg, editID, track string) (TrackState, error) {
	var current *androidpublisher.Track

	err := c.call(ctx, "tracks.get", c.attempts(2), func(ctx context.Context) error {
		got, err := c.svc.Edits.Tracks.Get(pkg, editID, track).Context(ctx).Do()
		if err != nil {
			return err
		}
		current = got
		return nil
	})
	if err != nil {
		return TrackState{}, err
	}

	state := TrackState{Track: track, Releases: make([]Release, 0, len(current.Releases))}
	for _, rel := range current.Releases {
		state.Releases = append(state.Releases, releaseFromAPI(rel))
	}
	return state, nil
}

func releaseFromAPI(rel *androidpublisher.TrackRelease) Release {
	out := Release{
		VersionCodes:        append([]int64(nil), rel.VersionCodes...),
		Status:              rel.Status,
		UserFraction:        rel.UserFraction,
		Name:                rel.Name,
		InAppUpdatePriority: rel.InAppUpdatePriority,
	}
	for _, note := range rel.ReleaseNotes {
		out.ReleaseNotes = append(out.ReleaseNotes, LocalizedText{Language: note.Language, Text: note.Text})
	}
	if rel.CountryTargeting != nil {
		out.CountryTargeting = &CountryTargeting{
			Countries:          append([]string(nil), rel.CountryTargeting.Countries...),
			IncludeRestOfWorld: rel.CountryTargeting.IncludeRestOfWorld,
		}
	}
	return out
}

func countryTargetingToAPI(targeting *CountryTargeting) *androidpublisher.CountryTargeting {
	if targeting == nil {
		return nil
	}
	return &androidpublisher.CountryTargeting{
		Countries:          targeting.Countries,
		IncludeRestOfWorld: targeting.IncludeRestOfWorld,
	}
}

// ValidateEdit validates an edit without committing it.
func (c *Client) ValidateEdit(ctx context.Context, pkg, editID string) error {
	return c.call(ctx, "edits.validate", c.retry.attempts, func(ctx context.Context) error {
		_, err := c.svc.Edits.Validate(pkg, editID).Context(ctx).Do()
		return err
	})
}

// CommitEdit applies an edit.
func (c *Client) CommitEdit(ctx context.Context, pkg, editID string, opts CommitOptions) error {
	call := c.svc.Edits.Commit(pkg, editID).Context(ctx)
	call.ChangesNotSentForReview(opts.ChangesNotSentForReview)
	if opts.InReviewBehavior != "" {
		call.ChangesInReviewBehavior(opts.InReviewBehavior)
	}

	if _, err := call.Do(); err != nil {
		// Cancellation can stop the response after Play received the commit.
		return mapCommitError(err)
	}
	return nil
}

// TrackReleases returns release summaries for one track without creating an edit.
func (c *Client) TrackReleases(ctx context.Context, pkg, track string) ([]ReleaseSummary, error) {
	parent := fmt.Sprintf("applications/%s/tracks/%s", pkg, track)

	var resp *androidpublisher.ListReleaseSummariesResponse
	err := c.call(ctx, "tracks.releases.list", c.retry.attempts, func(ctx context.Context) error {
		listed, err := c.svc.Applications.Tracks.Releases.List(parent).Context(ctx).Do()
		if err != nil {
			return err
		}
		resp = listed
		return nil
	})
	if err != nil {
		return nil, err
	}

	summaries := make([]ReleaseSummary, 0, len(resp.Releases))
	for _, rel := range resp.Releases {
		versions := make([]int64, 0, len(rel.ActiveArtifacts))
		for _, artifact := range rel.ActiveArtifacts {
			versions = append(versions, artifact.VersionCode)
		}
		summaries = append(summaries, ReleaseSummary{
			Track:        rel.Track,
			Name:         rel.ReleaseName,
			State:        rel.ReleaseLifecycleState,
			VersionCodes: versions,
		})
	}
	return summaries, nil
}

// progressUpdater logs upload progress every 5 percent at info level.
func (c *Client) progressUpdater(size int64) googleapi.ProgressUpdater {
	if size <= 0 {
		return nil
	}

	lastPercent := -5
	return func(current, _ int64) {
		percent := int(current * 100 / size)
		if percent-lastPercent < 5 {
			return
		}
		lastPercent = percent
		c.logger.Info("upload progress", "bytes", current, "total", size, "percent", percent)
	}
}

// UpdateListing replaces the localized store listing of one language.
func (c *Client) UpdateListing(ctx context.Context, pkg, editID string, listing Listing) error {
	update := &androidpublisher.Listing{
		Language:         listing.Language,
		Title:            listing.Title,
		ShortDescription: listing.ShortDescription,
		FullDescription:  listing.FullDescription,
		Video:            listing.Video,
	}

	return c.call(ctx, "listings.update", c.retry.attempts, func(ctx context.Context) error {
		_, err := c.svc.Edits.Listings.Update(pkg, editID, listing.Language, update).Context(ctx).Do()
		return err
	})
}

// DeleteAllImages removes every image of one type for a language. It runs
// before the images of that type are uploaded, so the metadata directory is
// authoritative; both happen inside one edit, so a failure leaves Play
// unchanged.
func (c *Client) DeleteAllImages(ctx context.Context, pkg, editID, language, imageType string) error {
	return c.call(ctx, "images.deleteall", c.retry.attempts, func(ctx context.Context) error {
		_, err := c.svc.Edits.Images.Deleteall(pkg, editID, language, imageType).Context(ctx).Do()
		return err
	})
}

// UploadImage uploads one image file. Uploads are not retried: a repeated
// upload would add a duplicate image to the edit.
func (c *Client) UploadImage(ctx context.Context, pkg, editID, language, imageType, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return exitcode.WithHint(exitcode.Wrap(exitcode.Usage, err), "check the image path")
	}
	defer func() { _ = file.Close() }()

	call := c.svc.Edits.Images.Upload(pkg, editID, language, imageType).Context(ctx)
	call.Media(file, googleapi.ContentType(imageContentType(path)))

	if _, err := call.Do(); err != nil {
		return mapError(err)
	}
	return nil
}

// UpdateTesters replaces the tester list of a track.
func (c *Client) UpdateTesters(ctx context.Context, pkg, editID string, testers Testers) error {
	update := &androidpublisher.Testers{GoogleGroups: testers.GoogleGroups}

	return c.call(ctx, "testers.update", c.retry.attempts, func(ctx context.Context) error {
		_, err := c.svc.Edits.Testers.Update(pkg, editID, testers.Track, update).Context(ctx).Do()
		return err
	})
}

// imageContentType maps an image extension to the upload media type.
func imageContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
}
