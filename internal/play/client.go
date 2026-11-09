package play

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	"cloud.google.com/go/auth/oauth2adapt"
	"golang.org/x/oauth2"
	"google.golang.org/api/androidpublisher/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

const (
	defaultChunkSize        = 8 << 20 // 8 MiB, a multiple of the required 256 KiB
	defaultChunkRetryWindow = 30 * time.Second
	defaultUserAgent        = "gplay-publisher"
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
}

// Client implements API on top of the official Google API client.
type Client struct {
	svc              *androidpublisher.Service
	logger           *slog.Logger
	chunkSize        int
	chunkRetryWindow time.Duration
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
		ts, err := tokenSource(opts.CredentialsFile)
		if err != nil {
			return nil, err
		}
		client = oauth2.NewClient(ctx, ts)
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
	}, nil
}

func userAgent(ua string) string {
	if ua == "" {
		return defaultUserAgent
	}
	return ua
}

func tokenSource(credentialsFile string) (oauth2.TokenSource, error) {
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
	return oauth2adapt.TokenSourceFromTokenProvider(creds), nil
}

// CreateEdit creates a new edit and returns its identifier.
func (c *Client) CreateEdit(ctx context.Context, pkg string) (string, error) {
	edit, err := c.svc.Edits.Insert(pkg, &androidpublisher.AppEdit{}).Context(ctx).Do()
	if err != nil {
		return "", mapError(err)
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
	call.Media(r,
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
			VersionCodes: googleapi.Int64s(rel.VersionCodes),
			Status:       rel.Status,
			UserFraction: rel.UserFraction,
			ReleaseNotes: notes,
			Name:         rel.Name,
		}},
	}

	if _, err := c.svc.Edits.Tracks.Update(pkg, editID, track, update).Context(ctx).Do(); err != nil {
		return mapError(err)
	}
	return nil
}

// ValidateEdit validates an edit without committing it.
func (c *Client) ValidateEdit(ctx context.Context, pkg, editID string) error {
	if _, err := c.svc.Edits.Validate(pkg, editID).Context(ctx).Do(); err != nil {
		return mapError(err)
	}
	return nil
}

// CommitEdit applies an edit.
func (c *Client) CommitEdit(ctx context.Context, pkg, editID string, opts CommitOptions) error {
	call := c.svc.Edits.Commit(pkg, editID).Context(ctx)
	call.ChangesNotSentForReview(opts.ChangesNotSentForReview)
	if opts.InReviewBehavior != "" {
		call.ChangesInReviewBehavior(opts.InReviewBehavior)
	}

	if _, err := call.Do(); err != nil {
		return mapError(err)
	}
	return nil
}

// TrackReleases returns release summaries for one track without creating an edit.
func (c *Client) TrackReleases(ctx context.Context, pkg, track string) ([]ReleaseSummary, error) {
	parent := fmt.Sprintf("applications/%s/tracks/%s", pkg, track)
	resp, err := c.svc.Applications.Tracks.Releases.List(parent).Context(ctx).Do()
	if err != nil {
		return nil, mapError(err)
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
