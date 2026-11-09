package play

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"

	"cloud.google.com/go/auth"
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

// AmbiguousFailure marks a failure whose request may have been applied by Play
// even though no definitive response was received. Callers that mutate state
// must reconcile these failures instead of trusting the error.
//
// Token retrieval failures are deliberately not marked: they happen before the
// API request is sent, so the mutation cannot have been applied.
type AmbiguousFailure struct {
	Err error
}

// Error implements the error interface.
func (e *AmbiguousFailure) Error() string { return e.Err.Error() }

// Unwrap exposes the original failure.
func (e *AmbiguousFailure) Unwrap() error { return e.Err }

// IsAmbiguous reports whether err marks a request whose effect may have been
// applied by Play.
func IsAmbiguous(err error) bool {
	var ambiguous *AmbiguousFailure
	return errors.As(err, &ambiguous)
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
		apiErr := &APIError{
			Status:  gerr.Code,
			Reason:  apiReason(gerr),
			Message: msg,
		}
		var cause error = apiErr
		// A server-side failure can happen before or after Play applied the
		// mutation, so the outcome is ambiguous. Client-side rejections are
		// definitive.
		if gerr.Code >= 500 {
			cause = &AmbiguousFailure{Err: apiErr}
		}
		mapped := exitcode.Wrap(code, cause)
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
		return exitcode.WithHint(
			exitcode.Wrap(exitcode.Transient, &AmbiguousFailure{Err: err}),
			hintFor(exitcode.Transient),
		)
	}

	// Token requests made by the context-aware auth transport surface as
	// *auth.Error when the endpoint answered; plain network errors were
	// handled by the switch above.
	var authErr *auth.Error
	if errors.As(err, &authErr) {
		return classifyAuthError(authErr)
	}

	return exitcode.Wrap(exitcode.Internal, err)
}

// mapCommitError classifies an edits.commit failure. Cancellation may stop the
// response after Play applied the edit, so it is marked ambiguous like a
// transport failure; definitive rejections keep the regular classification.
func mapCommitError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return exitcode.WithHint(
			exitcode.Wrap(exitcode.Interrupted, &AmbiguousFailure{Err: err}),
			"the commit may have been applied; check the track before retrying")
	}
	return mapError(err)
}

// classifyAuthError maps a failed token request from the cloud auth transport.
// A rejected credential is an authentication problem; an endpoint outage or
// rate limit is transient.
func classifyAuthError(err *auth.Error) error {
	status := 0
	if err.Response != nil {
		status = err.Response.StatusCode
	}

	code := exitcode.Transient
	if status >= 400 && status < 500 && status != http.StatusTooManyRequests {
		code = exitcode.Auth
	}

	detail := "auth error"
	switch {
	case err.Response != nil:
		detail = err.Error()
	case err.Err != nil:
		detail = err.Err.Error()
	}
	msg := "token retrieval failed: " + detail
	if status != 0 {
		msg = fmt.Sprintf("%s (HTTP %d)", msg, status)
	}
	return exitcode.WithHint(exitcode.Wrap(code, errors.New(msg)), hintFor(code))
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
