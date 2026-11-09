package play_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/play"
)

func TestDebugTransportLogsExchange(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Goog-Request-Id", "req-123")
		fmt.Fprint(w, `{"id":"edit-1"}`)
	})

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	var logs bytes.Buffer
	client, err := play.New(context.Background(), play.Options{
		HTTPClient: srv.Client(),
		Endpoint:   srv.URL,
		Debug:      true,
		Logger:     slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatalf("play.New: %v", err)
	}

	if _, err := client.CreateEdit(context.Background(), "com.example.app"); err != nil {
		t.Fatalf("CreateEdit: %v", err)
	}

	out := logs.String()
	for _, want := range []string{`msg="http response"`, "status=200", "request_id=req-123", `response_body=`} {
		if !strings.Contains(out, want) {
			t.Fatalf("debug log misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Authorization") {
		t.Fatalf("debug log leaks Authorization header:\n%s", out)
	}
}

func TestDebugTransportRedactsSecrets(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"edit-1","private_key":"super-secret","client_secret":"also-secret"}`)
	})

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	var logs bytes.Buffer
	client, err := play.New(context.Background(), play.Options{
		HTTPClient: srv.Client(),
		Endpoint:   srv.URL,
		Debug:      true,
		Logger:     slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatalf("play.New: %v", err)
	}

	if _, err := client.CreateEdit(context.Background(), "com.example.app"); err != nil {
		t.Fatalf("CreateEdit: %v", err)
	}

	out := logs.String()
	if strings.Contains(out, "super-secret") || strings.Contains(out, "also-secret") {
		t.Fatalf("debug log leaks secrets:\n%s", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("debug log does not mark redacted fields:\n%s", out)
	}
}

func TestNoDebugLogs(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"edit-1"}`)
	})

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	var logs bytes.Buffer
	client, err := play.New(context.Background(), play.Options{
		HTTPClient: srv.Client(),
		Endpoint:   srv.URL,
		Logger:     slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatalf("play.New: %v", err)
	}

	if _, err := client.CreateEdit(context.Background(), "com.example.app"); err != nil {
		t.Fatalf("CreateEdit: %v", err)
	}
	if logs.Len() != 0 {
		t.Fatalf("expected no logs without --debug, got:\n%s", logs.String())
	}
}
