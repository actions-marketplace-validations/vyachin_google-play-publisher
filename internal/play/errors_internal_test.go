package play

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"testing"

	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

func TestMapErrorNil(t *testing.T) {
	if err := mapError(nil); err != nil {
		t.Fatalf("mapError(nil) = %v, want nil", err)
	}
}

func TestMapErrorNetworkAndTimeout(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want exitcode.Code
	}{
		{"deadline", context.DeadlineExceeded, exitcode.Transient},
		{"wrapped deadline", fmt.Errorf("upload: %w", context.DeadlineExceeded), exitcode.Transient},
		{"url error", &url.Error{Op: "Post", URL: "https://example.com", Err: context.DeadlineExceeded}, exitcode.Transient},
		{"eof", io.EOF, exitcode.Transient},
		{"unexpected eof", io.ErrUnexpectedEOF, exitcode.Transient},
		{"connection reset", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, exitcode.Transient},
		{"timeout", &net.OpError{Op: "dial", Net: "tcp", Err: timeoutError{}}, exitcode.Transient},
		{"canceled", context.Canceled, exitcode.Interrupted},
		{"unknown", errors.New("boom"), exitcode.Internal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := mapError(test.err)
			if got := exitcode.From(err); got != test.want {
				t.Fatalf("code = %d, want %d (%v)", got, test.want, err)
			}
			if test.want == exitcode.Transient || test.want == exitcode.Auth {
				var coded *exitcode.Error
				if !errors.As(err, &coded) || coded.Hint == "" {
					t.Fatalf("expected a hint on %v", err)
				}
			}
		})
	}
}

func TestMapErrorTokenRetrieval(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   exitcode.Code
	}{
		{"rejected credentials", http.StatusUnauthorized, exitcode.Auth},
		{"forbidden", http.StatusForbidden, exitcode.Auth},
		{"invalid grant", http.StatusBadRequest, exitcode.Auth},
		{"rate limited", http.StatusTooManyRequests, exitcode.Transient},
		{"provider outage", http.StatusBadGateway, exitcode.Transient},
		{"network", 0, exitcode.Transient},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			oauthErr := &oauth2.RetrieveError{ErrorCode: "invalid_grant", ErrorDescription: "bad key"}
			if test.status != 0 {
				oauthErr.Response = &http.Response{StatusCode: test.status}
			}

			err := mapError(oauthErr)
			if got := exitcode.From(err); got != test.want {
				t.Fatalf("code = %d, want %d (%v)", got, test.want, err)
			}
		})
	}
}

func TestMapErrorKeepsAPIReason(t *testing.T) {
	apiErr := &googleapi.Error{
		Code:    http.StatusBadRequest,
		Message: "the version code 42 has already been used",
		Errors:  []googleapi.ErrorItem{{Reason: "badRequest", Message: "already used"}},
	}

	err := mapError(apiErr)
	if got := exitcode.From(err); got != exitcode.Conflict {
		t.Fatalf("code = %d, want %d", got, exitcode.Conflict)
	}

	var structured *APIError
	if !errors.As(err, &structured) {
		t.Fatalf("APIError not retained: %v", err)
	}
	if structured.Status != http.StatusBadRequest || structured.Reason != "badRequest" {
		t.Fatalf("unexpected APIError: %#v", structured)
	}
	if got := structured.Error(); got != "the version code 42 has already been used (HTTP 400)" {
		t.Fatalf("unexpected message: %q", got)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
