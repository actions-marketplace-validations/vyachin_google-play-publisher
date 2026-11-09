package play

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"

	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

// APIError is a structured Play API failure. It keeps the HTTP status and the
// Play reason so callers can render them without parsing the message.
type APIError struct {
	Status  int
	Reason  string
	Message string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Status == 0 {
		return e.Message
	}
	return fmt.Sprintf("%s (HTTP %d)", e.Message, e.Status)
}

// mapError converts Google API and transport errors into coded errors with hints.
func mapError(err error) error {
	if err == nil {
		return nil
	}

	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		code := classify(gerr)
		msg := gerr.Message
		if msg == "" {
			msg = gerr.Error()
		}
		mapped := exitcode.Wrap(code, &APIError{
			Status:  gerr.Code,
			Reason:  apiReason(gerr),
			Message: msg,
		})
		return exitcode.WithHint(mapped, hintFor(code))
	}

	// Token retrieval happens before the API call, so it never produces a
	// googleapi.Error. Keep those failures separate from API failures.
	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) {
		return classifyRetrieveError(retrieveErr)
	}

	switch {
	case errors.Is(err, context.Canceled):
		return exitcode.Wrap(exitcode.Interrupted, err)
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, io.EOF),
		errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.ECONNREFUSED),
		errors.Is(err, syscall.EHOSTUNREACH),
		errors.Is(err, syscall.ENETUNREACH),
		errors.Is(err, syscall.EPIPE),
		isTimeout(err):
		return exitcode.WithHint(exitcode.Wrap(exitcode.Transient, err), hintFor(exitcode.Transient))
	}

	return exitcode.Wrap(exitcode.Internal, err)
}

// classifyRetrieveError maps a failed token request. A rejected credential is
// an authentication problem; a network failure or a provider outage is
// transient.
func classifyRetrieveError(err *oauth2.RetrieveError) error {
	status := 0
	if err.Response != nil {
		status = err.Response.StatusCode
	}

	code := exitcode.Transient
	if status >= 400 && status < 500 && status != 429 {
		code = exitcode.Auth
	}

	msg := "token retrieval failed: " + err.Error()
	if status != 0 {
		msg = fmt.Sprintf("%s (HTTP %d)", msg, status)
	}
	return exitcode.WithHint(exitcode.Wrap(code, errors.New(msg)), hintFor(code))
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func apiReason(gerr *googleapi.Error) string {
	for _, item := range gerr.Errors {
		if item.Reason != "" {
			return item.Reason
		}
	}
	return ""
}

func classify(gerr *googleapi.Error) exitcode.Code {
	msg := strings.ToLower(gerr.Message)

	switch gerr.Code {
	case 400:
		switch {
		case strings.Contains(msg, "version code") && strings.Contains(msg, "already been used"):
			return exitcode.Conflict
		case strings.Contains(msg, "changes cannot be sent for review automatically"):
			return exitcode.ManualAction
		case strings.Contains(msg, "already in review"):
			return exitcode.Conflict
		case strings.Contains(msg, "cannot find edit"), strings.Contains(msg, "edit is not valid"), strings.Contains(msg, "invalid edit"):
			return exitcode.Conflict
		default:
			return exitcode.Rejected
		}
	case 401:
		return exitcode.Auth
	case 403:
		if strings.Contains(msg, "daily edit creation quota") {
			return exitcode.Transient
		}
		return exitcode.Auth
	case 404:
		return exitcode.NotFound
	case 409:
		return exitcode.Conflict
	case 429:
		return exitcode.Transient
	}

	if gerr.Code >= 500 {
		return exitcode.Transient
	}
	return exitcode.Internal
}

func hintFor(code exitcode.Code) string {
	switch code {
	case exitcode.Auth:
		return "check service account permissions in Play Console (Users and permissions)"
	case exitcode.NotFound:
		return "check the applicationId and that the service account has access to the app"
	case exitcode.Conflict:
		return "reload the app state and retry; bump versionCode if it was already used"
	case exitcode.Transient:
		return "retry later; check Google Play status if the error persists"
	case exitcode.ManualAction:
		return "send the changes for review from Play Console (Publishing overview)"
	}
	return ""
}
