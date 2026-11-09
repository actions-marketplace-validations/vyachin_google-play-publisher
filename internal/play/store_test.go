package play_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
)

func TestUpdateListing(t *testing.T) {
	var (
		gotMethod, gotPath string
		body               map[string]any
	)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"language":"en-US","title":"Example"}`)
	})

	client, _ := newClient(t, h)
	err := client.UpdateListing(context.Background(), "com.example.app", "edit-1", play.Listing{
		Language:         "en-US",
		Title:            "Example",
		ShortDescription: "Short",
		FullDescription:  "Full description",
		Video:            "https://www.youtube.com/watch?v=abc",
	})
	if err != nil {
		t.Fatalf("UpdateListing: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", gotMethod)
	}
	wantPath := "/androidpublisher/v3/applications/com.example.app/edits/edit-1/listings/en-US"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if body["title"] != "Example" || body["shortDescription"] != "Short" ||
		body["fullDescription"] != "Full description" {
		t.Errorf("body = %#v", body)
	}
	if body["language"] != "en-US" {
		t.Errorf("language = %v, want en-US", body["language"])
	}
}

func TestDeleteAllImages(t *testing.T) {
	var gotMethod, gotPath string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	})

	client, _ := newClient(t, h)
	if err := client.DeleteAllImages(context.Background(), "com.example.app", "edit-1", "en-US", "phoneScreenshots"); err != nil {
		t.Fatalf("DeleteAllImages: %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	wantPath := "/androidpublisher/v3/applications/com.example.app/edits/edit-1/listings/en-US/phoneScreenshots"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
}

func TestUploadImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "icon.png")
	payload := []byte("png-bytes")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}

	var (
		gotPath, gotType, gotQuery string
		gotBody                    []byte
	)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotType, gotQuery = r.URL.Path, r.Header.Get("Content-Type"), r.URL.Query().Get("uploadType")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"image-1"}`)
	})

	client, _ := newClient(t, h)
	if err := client.UploadImage(context.Background(), "com.example.app", "edit-1", "en-US", "appIcon", path); err != nil {
		t.Fatalf("UploadImage: %v", err)
	}

	if gotPath != "/upload/androidpublisher/v3/applications/com.example.app/edits/edit-1/listings/en-US/appIcon" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "multipart" {
		t.Errorf("uploadType = %q, want multipart", gotQuery)
	}
	if !strings.HasPrefix(gotType, "multipart/related") {
		t.Errorf("content type = %q, want multipart/related", gotType)
	}
	if !strings.Contains(string(gotBody), string(payload)) {
		t.Errorf("upload body does not contain the image bytes")
	}
}

func TestUploadImageMissingFile(t *testing.T) {
	client, _ := newClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	err := client.UploadImage(context.Background(), "com.example.app", "edit-1", "en-US", "appIcon",
		filepath.Join(t.TempDir(), "missing.png"))
	if code := exitcode.From(err); code != exitcode.Usage {
		t.Fatalf("code = %d, want %d: %v", code, exitcode.Usage, err)
	}
}

func TestUpdateTesters(t *testing.T) {
	var (
		gotMethod, gotPath string
		body               map[string]any
	)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"googleGroups":["qa@googlegroups.com"]}`)
	})

	client, _ := newClient(t, h)
	err := client.UpdateTesters(context.Background(), "com.example.app", "edit-1", play.Testers{
		Track:        "beta",
		GoogleGroups: []string{"qa@googlegroups.com"},
	})
	if err != nil {
		t.Fatalf("UpdateTesters: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", gotMethod)
	}
	wantPath := "/androidpublisher/v3/applications/com.example.app/edits/edit-1/testers/beta"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	groups, ok := body["googleGroups"].([]any)
	if !ok || len(groups) != 1 || groups[0] != "qa@googlegroups.com" {
		t.Errorf("googleGroups = %#v", body["googleGroups"])
	}
}
