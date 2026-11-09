package play_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

func TestStoreListings(t *testing.T) {
	var gotMethod, gotPath string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"listings":[
			{"language":"fr-FR","title":"Titre","shortDescription":"Court","fullDescription":"Description"},
			{"language":"en-US","title":"Example","shortDescription":"Short","fullDescription":"Full","video":"https://youtu.be/x"}
		]}`)
	})

	client, _ := newClient(t, h)
	listings, err := client.StoreListings(context.Background(), "com.example.app", "edit-1")
	if err != nil {
		t.Fatalf("StoreListings: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	wantPath := "/androidpublisher/v3/applications/com.example.app/edits/edit-1/listings"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if len(listings) != 2 || listings[0].Language != "en-US" || listings[1].Language != "fr-FR" {
		t.Fatalf("listings = %+v, want sorted by language", listings)
	}
	if listings[0].Video != "https://youtu.be/x" || listings[1].Title != "Titre" {
		t.Errorf("unexpected listings: %+v", listings)
	}
}

func TestStoreImagesUsesFullSizeURL(t *testing.T) {
	var gotPath string
	var imageURL string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"images":[{"id":"image-1","sha256":"abc","url":%q}]}`, imageURL)
	})

	client, srv := newClient(t, h)
	imageURL = srv.URL + "/preview"
	images, err := client.StoreImages(context.Background(), "com.example.app", "edit-1", "en-US", "phoneScreenshots")
	if err != nil {
		t.Fatalf("StoreImages: %v", err)
	}

	wantPath := "/androidpublisher/v3/applications/com.example.app/edits/edit-1/listings/en-US/phoneScreenshots"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if len(images) != 1 || images[0].ID != "image-1" || images[0].SHA256 != "abc" {
		t.Fatalf("images = %+v", images)
	}
	if images[0].URL != imageURL+"=s0" {
		t.Errorf("url = %q, want %q", images[0].URL, imageURL+"=s0")
	}
}

func TestTesterGroups(t *testing.T) {
	var gotPath string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"googleGroups":["qa@googlegroups.com","beta@googlegroups.com"]}`)
	})

	client, _ := newClient(t, h)
	testers, err := client.TesterGroups(context.Background(), "com.example.app", "edit-1", "internal")
	if err != nil {
		t.Fatalf("TesterGroups: %v", err)
	}

	wantPath := "/androidpublisher/v3/applications/com.example.app/edits/edit-1/testers/internal"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if testers.Track != "internal" || len(testers.GoogleGroups) != 2 {
		t.Fatalf("testers = %+v", testers)
	}
}

func TestAllTracks(t *testing.T) {
	var gotPath string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tracks":[
			{"track":"beta","releases":[{"versionCodes":["41"],"status":"completed","releaseNotes":[{"language":"en-US","text":"Fixes"}]}]},
			{"track":"internal","releases":[]}
		]}`)
	})

	client, _ := newClient(t, h)
	tracks, err := client.AllTracks(context.Background(), "com.example.app", "edit-1")
	if err != nil {
		t.Fatalf("AllTracks: %v", err)
	}

	wantPath := "/androidpublisher/v3/applications/com.example.app/edits/edit-1/tracks"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if len(tracks) != 2 || tracks[0].Track != "beta" || tracks[1].Track != "internal" {
		t.Fatalf("tracks = %+v, want sorted by name", tracks)
	}
	if len(tracks[0].Releases) != 1 || len(tracks[0].Releases[0].ReleaseNotes) != 1 {
		t.Fatalf("release = %+v", tracks[0].Releases)
	}
	if tracks[0].Releases[0].ReleaseNotes[0].Text != "Fixes" {
		t.Errorf("notes = %+v", tracks[0].Releases[0].ReleaseNotes)
	}
}

func TestDownloadImage(t *testing.T) {
	var payload bytes.Buffer
	if err := png.Encode(&payload, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	var gotAuthorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(payload.Bytes())
	}))
	defer srv.Close()

	client, _ := newClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	data, err := client.DownloadImage(context.Background(), srv.URL+"/image.png")
	if err != nil {
		t.Fatalf("DownloadImage: %v", err)
	}
	if !bytes.Equal(data, payload.Bytes()) {
		t.Fatalf("downloaded %d bytes, want %d", len(data), payload.Len())
	}
	if gotAuthorization != "" {
		t.Errorf("image CDN received Authorization %q", gotAuthorization)
	}
}

func TestDownloadImageErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   exitcode.Code
	}{
		{name: "not found", status: http.StatusNotFound, want: exitcode.NotFound},
		{name: "forbidden", status: http.StatusForbidden, want: exitcode.Rejected},
		{name: "server error", status: http.StatusInternalServerError, want: exitcode.Transient},
		{name: "rate limited", status: http.StatusTooManyRequests, want: exitcode.Transient},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			client, _ := newClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			_, err := client.DownloadImage(context.Background(), srv.URL+"/image.png")
			if code := exitcode.From(err); code != tt.want {
				t.Fatalf("code = %d, want %d: %v", code, tt.want, err)
			}
		})
	}
}

func TestDownloadImageRejectsInvalidURL(t *testing.T) {
	client, _ := newClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	for _, raw := range []string{"", "file:///etc/passwd", "not a url"} {
		_, err := client.DownloadImage(context.Background(), raw)
		if code := exitcode.From(err); code != exitcode.Internal {
			t.Fatalf("%q: code = %d, want %d: %v", raw, code, exitcode.Internal, err)
		}
	}
}

func TestStoreListingsError(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"code":403,"message":"forbidden"}}`)
	})

	client, _ := newClient(t, h)
	_, err := client.StoreListings(context.Background(), "com.example.app", "edit-1")
	if code := exitcode.From(err); code != exitcode.Auth && code != exitcode.Rejected {
		t.Fatalf("code = %d, want auth or rejected: %v", code, err)
	}
	if !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDownloadImageCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client, _ := newClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	_, err := client.DownloadImage(ctx, srv.URL+"/image.png")
	if code := exitcode.From(err); code != exitcode.Interrupted {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Interrupted, err)
	}
}
