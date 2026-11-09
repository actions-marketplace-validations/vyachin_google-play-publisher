package play_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
)

func newClient(t *testing.T, h http.Handler) (*play.Client, *httptest.Server) {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	client, err := play.New(context.Background(), play.Options{
		HTTPClient: srv.Client(),
		Endpoint:   srv.URL,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		// Error tests assert single-call classification; retries have their
		// own tests with an injected sleep.
		Retry: play.RetryOptions{Attempts: 1},
	})
	if err != nil {
		t.Fatalf("play.New: %v", err)
	}
	return client, srv
}

func TestCreateEdit(t *testing.T) {
	var gotMethod, gotPath, gotUserAgent string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"edit-1","expiryTimeSeconds":"123"}`)
	})

	client, _ := newClient(t, h)
	id, err := client.CreateEdit(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("CreateEdit: %v", err)
	}

	if id != "edit-1" {
		t.Fatalf("edit id = %q, want %q", id, "edit-1")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := "/androidpublisher/v3/applications/com.example.app/edits"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if !strings.HasPrefix(gotUserAgent, "google-play-publisher") {
		t.Errorf("user agent = %q, want google-play-publisher prefix", gotUserAgent)
	}
}

func TestCreateEditErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantCode   exitcode.Code
		wantInText string
	}{
		{
			name:       "unauthorized",
			status:     http.StatusUnauthorized,
			body:       `{"error":{"code":401,"message":"Request had invalid authentication credentials"}}`,
			wantCode:   exitcode.Auth,
			wantInText: "invalid authentication",
		},
		{
			name:       "forbidden",
			status:     http.StatusForbidden,
			body:       `{"error":{"code":403,"message":"The caller does not have permission"}}`,
			wantCode:   exitcode.Auth,
			wantInText: "does not have permission",
		},
		{
			name:       "not found",
			status:     http.StatusNotFound,
			body:       `{"error":{"code":404,"message":"Application not found"}}`,
			wantCode:   exitcode.NotFound,
			wantInText: "not found",
		},
		{
			name:       "conflict",
			status:     http.StatusConflict,
			body:       `{"error":{"code":409,"message":"Conflict"}}`,
			wantCode:   exitcode.Conflict,
			wantInText: "Conflict",
		},
		{
			name:       "rate limited",
			status:     http.StatusTooManyRequests,
			body:       `{"error":{"code":429,"message":"Rate limit exceeded"}}`,
			wantCode:   exitcode.Transient,
			wantInText: "Rate limit",
		},
		{
			name:       "server error",
			status:     http.StatusInternalServerError,
			body:       `{"error":{"code":500,"message":"Internal error"}}`,
			wantCode:   exitcode.Transient,
			wantInText: "Internal error",
		},
		{
			name:       "version code used",
			status:     http.StatusBadRequest,
			body:       `{"error":{"code":400,"message":"Version code 42 has already been used"}}`,
			wantCode:   exitcode.Conflict,
			wantInText: "already been used",
		},
		{
			name:       "validation error",
			status:     http.StatusBadRequest,
			body:       `{"error":{"code":400,"message":"Bundle is not valid"}}`,
			wantCode:   exitcode.Rejected,
			wantInText: "not valid",
		},
		{
			name:       "manual review required",
			status:     http.StatusBadRequest,
			body:       `{"error":{"code":400,"message":"Changes cannot be sent for review automatically"}}`,
			wantCode:   exitcode.ManualAction,
			wantInText: "cannot be sent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			})

			client, _ := newClient(t, h)
			_, err := client.CreateEdit(context.Background(), "com.example.app")
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := exitcode.From(err); got != tt.wantCode {
				t.Fatalf("code = %d, want %d (err: %v)", got, tt.wantCode, err)
			}
			if !strings.Contains(err.Error(), tt.wantInText) {
				t.Fatalf("error %q does not contain %q", err, tt.wantInText)
			}
		})
	}
}

func TestDeleteEditIgnoresNotFound(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"code":404,"message":"Cannot find edit"}}`)
	})

	client, _ := newClient(t, h)
	if err := client.DeleteEdit(context.Background(), "com.example.app", "missing"); err != nil {
		t.Fatalf("DeleteEdit returned error: %v", err)
	}
}

func TestCommitEditOptions(t *testing.T) {
	var gotQuery string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"edit-1"}`)
	})

	client, _ := newClient(t, h)
	err := client.CommitEdit(context.Background(), "com.example.app", "edit-1", play.CommitOptions{
		ChangesNotSentForReview: true,
		InReviewBehavior:        play.InReviewError,
	})
	if err != nil {
		t.Fatalf("CommitEdit: %v", err)
	}

	if !strings.Contains(gotQuery, "changesNotSentForReview=true") {
		t.Errorf("query %q misses changesNotSentForReview", gotQuery)
	}
	if !strings.Contains(gotQuery, "changesInReviewBehavior=ERROR_IF_IN_REVIEW") {
		t.Errorf("query %q misses changesInReviewBehavior", gotQuery)
	}
}

func TestUpdateTrack(t *testing.T) {
	var body map[string]any
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"track":"internal","releases":[]}`)
	})

	client, _ := newClient(t, h)
	err := client.UpdateTrack(context.Background(), "com.example.app", "edit-1", "internal", play.Release{
		VersionCodes: []int64{42},
		Status:       play.StatusCompleted,
		ReleaseNotes: []play.LocalizedText{{Language: "en-US", Text: "Fixes"}},
	})
	if err != nil {
		t.Fatalf("UpdateTrack: %v", err)
	}

	releases, ok := body["releases"].([]any)
	if !ok || len(releases) != 1 {
		t.Fatalf("unexpected releases: %#v", body["releases"])
	}
	rel := releases[0].(map[string]any)
	if rel["status"] != "completed" {
		t.Errorf("status = %v, want completed", rel["status"])
	}
	versions, ok := rel["versionCodes"].([]any)
	if !ok || len(versions) != 1 || versions[0] != "42" {
		t.Errorf("versionCodes = %#v, want [\"42\"]", rel["versionCodes"])
	}
}

func TestUploadBundleMultipart(t *testing.T) {
	var gotType string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.URL.Query().Get("uploadType")
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"versionCode":42,"sha1":"sha1-value","sha256":"sha256-value"}`)
	})

	client, _ := newClient(t, h)
	payload := []byte("aab-bytes")
	info, err := client.UploadBundle(context.Background(), "com.example.app", "edit-1",
		bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("UploadBundle: %v", err)
	}

	if gotType != "multipart" {
		t.Errorf("uploadType = %q, want multipart", gotType)
	}
	if info.VersionCode != 42 || info.SHA256 != "sha256-value" {
		t.Fatalf("unexpected bundle info: %#v", info)
	}
}

func TestUploadBundleResumable(t *testing.T) {
	const chunkSize = 8 << 20
	const total = chunkSize + 1024

	var chunks int
	mux := http.NewServeMux()
	mux.HandleFunc("/upload/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("uploadType"); got != "resumable" {
			t.Errorf("uploadType = %q, want resumable", got)
		}
		w.Header().Set("Location", "http://"+r.Host+"/session/1")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/session/1", func(w http.ResponseWriter, r *http.Request) {
		chunks++
		_, _ = io.Copy(io.Discard, r.Body)

		start, end, size := parseContentRange(r.Header.Get("Content-Range"))
		if size < 0 {
			// Intermediate chunk: emulate Google, which answers 200 with an
			// override header when the client asked to avoid a literal 308.
			w.Header().Set("X-HTTP-Status-Code-Override", "308")
			w.Header().Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"versionCode":42,"sha1":"sha1-value","sha256":"sha256-value"}`)
	})

	client, _ := newClient(t, mux)
	info, err := client.UploadBundle(context.Background(), "com.example.app", "edit-1",
		bytes.NewReader(bytes.Repeat([]byte("a"), total)), total)
	if err != nil {
		t.Fatalf("UploadBundle: %v", err)
	}

	if chunks != 2 {
		t.Errorf("chunks = %d, want 2", chunks)
	}
	if info.VersionCode != 42 {
		t.Fatalf("unexpected bundle info: %#v", info)
	}
}

// parseContentRange parses "bytes 0-8388607/*" or "bytes 0-8388607/9437184".
// It returns size = -1 when the total is unknown.
func parseContentRange(header string) (start, end, size int64) {
	header = strings.TrimPrefix(header, "bytes ")
	parts := strings.Split(header, "/")
	if len(parts) != 2 {
		return 0, 0, -1
	}
	span := strings.Split(parts[0], "-")
	if len(span) != 2 {
		return 0, 0, -1
	}
	start, _ = strconv.ParseInt(span[0], 10, 64)
	end, _ = strconv.ParseInt(span[1], 10, 64)
	if parts[1] == "*" {
		return start, end, -1
	}
	size, _ = strconv.ParseInt(parts[1], 10, 64)
	return start, end, size
}

func TestTrackReleases(t *testing.T) {
	var gotPath string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"releases":[{
			"track":"internal",
			"releaseName":"1.0",
			"releaseLifecycleState":"RELEASE_LIFECYCLE_STATE_PUBLISHED",
			"activeArtifacts":[{"versionCode":42},{"versionCode":43}]
		}]}`)
	})

	client, _ := newClient(t, h)
	releases, err := client.TrackReleases(context.Background(), "com.example.app", "internal")
	if err != nil {
		t.Fatalf("TrackReleases: %v", err)
	}

	if want := "/androidpublisher/v3/applications/com.example.app/tracks/internal/releases"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(releases) != 1 {
		t.Fatalf("releases = %d, want 1", len(releases))
	}
	if releases[0].State != "RELEASE_LIFECYCLE_STATE_PUBLISHED" {
		t.Errorf("state = %q", releases[0].State)
	}
	if len(releases[0].VersionCodes) != 2 || releases[0].VersionCodes[1] != 43 {
		t.Errorf("version codes = %v", releases[0].VersionCodes)
	}
}

func TestAuthErrorForMissingCredentialsFile(t *testing.T) {
	_, err := play.New(context.Background(), play.Options{CredentialsFile: "/nonexistent/key.json"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Auth {
		t.Fatalf("code = %d, want %d", got, exitcode.Auth)
	}
}
