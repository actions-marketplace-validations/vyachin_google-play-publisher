package play

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/api/googleapi"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

// mapError converts Google API errors into coded errors with user hints.
func mapError(err error) error {
	if err == nil {
		return nil
	}

	var gerr *googleapi.Error
	if !errors.As(err, &gerr) {
		return exitcode.Wrap(exitcode.Internal, err)
	}

	code := classify(gerr)
	msg := gerr.Message
	if msg == "" {
		msg = gerr.Error()
	}

	mapped := exitcode.Wrap(code, fmt.Errorf("%s (HTTP %d)", msg, gerr.Code))
	return exitcode.WithHint(mapped, hintFor(code))
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
