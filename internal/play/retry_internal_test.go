package play

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"google.golang.org/api/googleapi"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

func newRetryTestClient(attempts int, sleep func(context.Context, time.Duration) error) *Client {
	return &Client{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		retry: retryConfig{
			attempts: attempts,
			base:     time.Second,
			cap:      32 * time.Second,
			sleep:    sleep,
		},
	}
}

func recordSleeps(sleeps *[]time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, d time.Duration) error {
		*sleeps = append(*sleeps, d)
		return nil
	}
}

func TestCallRetriesTransientFailures(t *testing.T) {
	var sleeps []time.Duration
	client := newRetryTestClient(5, recordSleeps(&sleeps))

	calls := 0
	err := client.call(context.Background(), "test", 5, func(context.Context) error {
		calls++
		if calls < 3 {
			return &googleapi.Error{Code: http.StatusServiceUnavailable, Message: "backend unavailable"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if len(sleeps) != 2 {
		t.Fatalf("sleeps = %v, want 2", sleeps)
	}

	// First retry is 1s, second 2s, both plus up to 1s of jitter.
	for i, min := range []time.Duration{time.Second, 2 * time.Second} {
		if sleeps[i] < min || sleeps[i] >= min+time.Second {
			t.Fatalf("sleep[%d] = %s, want [%s, %s)", i, sleeps[i], min, min+time.Second)
		}
	}
}

func TestCallHonorsRetryAfter(t *testing.T) {
	var sleeps []time.Duration
	client := newRetryTestClient(5, recordSleeps(&sleeps))

	calls := 0
	err := client.call(context.Background(), "test", 5, func(context.Context) error {
		calls++
		if calls == 1 {
			return &googleapi.Error{
				Code:    http.StatusTooManyRequests,
				Message: "rate limited",
				Header:  http.Header{"Retry-After": {"7"}},
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(sleeps) != 1 || sleeps[0] != 7*time.Second {
		t.Fatalf("sleeps = %v, want exactly 7s", sleeps)
	}
}

func TestCallDoesNotRetryValidationErrors(t *testing.T) {
	var sleeps []time.Duration
	client := newRetryTestClient(5, recordSleeps(&sleeps))

	calls := 0
	err := client.call(context.Background(), "test", 5, func(context.Context) error {
		calls++
		return &googleapi.Error{Code: http.StatusBadRequest, Message: "bundle is not valid"}
	})
	if got := exitcode.From(err); got != exitcode.Rejected {
		t.Fatalf("code = %d, want %d", got, exitcode.Rejected)
	}
	if calls != 1 || len(sleeps) != 0 {
		t.Fatalf("calls = %d, sleeps = %v, want no retries", calls, sleeps)
	}
}

func TestCallExhaustsAttempts(t *testing.T) {
	var sleeps []time.Duration
	client := newRetryTestClient(3, recordSleeps(&sleeps))

	calls := 0
	err := client.call(context.Background(), "test", 3, func(context.Context) error {
		calls++
		return &googleapi.Error{Code: http.StatusServiceUnavailable, Message: "backend unavailable"}
	})
	if got := exitcode.From(err); got != exitcode.Transient {
		t.Fatalf("code = %d, want %d", got, exitcode.Transient)
	}
	if calls != 3 || len(sleeps) != 2 {
		t.Fatalf("calls = %d, sleeps = %v, want 3 attempts", calls, sleeps)
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Fatalf("error %q does not report the attempt count", err)
	}

	// The structured API error survives for the JSON error object.
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("APIError is lost: %v", err)
	}
}

func TestCallStopsWhenContextIsDone(t *testing.T) {
	client := newRetryTestClient(3, func(context.Context, time.Duration) error {
		return context.Canceled
	})

	calls := 0
	err := client.call(context.Background(), "test", 3, func(context.Context) error {
		calls++
		return &googleapi.Error{Code: http.StatusServiceUnavailable, Message: "backend unavailable"}
	})
	if got := exitcode.From(err); got != exitcode.Interrupted {
		t.Fatalf("code = %d, want %d", got, exitcode.Interrupted)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestClientRetriesCreateEdit(t *testing.T) {
	requests := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":{"code":503,"message":"backend unavailable"}}`)
			return
		}
		fmt.Fprint(w, `{"id":"edit-1"}`)
	})

	server := httptest.NewServer(h)
	t.Cleanup(server.Close)

	client, err := New(context.Background(), Options{
		HTTPClient: server.Client(),
		Endpoint:   server.URL,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.retry.sleep = func(context.Context, time.Duration) error { return nil }

	editID, err := client.CreateEdit(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("CreateEdit: %v", err)
	}
	if editID != "edit-1" {
		t.Fatalf("edit id = %q, want edit-1", editID)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestCallRetriesConnectionRefused(t *testing.T) {
	var sleeps []time.Duration
	client := newRetryTestClient(5, recordSleeps(&sleeps))

	calls := 0
	err := client.call(context.Background(), "test", 5, func(context.Context) error {
		calls++
		if calls < 3 {
			return &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: os.NewSyscallError("connect", syscall.ECONNREFUSED),
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if len(sleeps) != 2 {
		t.Fatalf("sleeps = %v, want 2", sleeps)
	}
}
