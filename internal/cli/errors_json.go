package cli

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
)

// errorDetail is the stable machine-readable error object from
// docs/03-cli-contract.md §5.
type errorDetail struct {
	Code     string `json:"code"`
	ExitCode int    `json:"exitCode"`
	Message  string `json:"message"`
	APICode  int    `json:"apiCode,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Hint     string `json:"hint,omitempty"`
}

type errorOutput struct {
	OK      bool        `json:"ok"`
	Command string      `json:"command"`
	Package string      `json:"package,omitempty"`
	Error   errorDetail `json:"error"`
}

// renderedError marks an error whose JSON result has already been written to
// stdout, so Execute must not emit a second object.
type renderedError struct {
	err error
}

func (e *renderedError) Error() string { return e.err.Error() }

func (e *renderedError) Unwrap() error { return e.err }

// writeErrorJSON renders the single JSON error object for a failed command.
func writeErrorJSON(w io.Writer, cmd *cobra.Command, err error, code exitcode.Code) error {
	out := errorOutput{
		OK:      false,
		Command: commandName(cmd),
		Package: commandPackage(cmd),
		Error:   newErrorDetail(err, code),
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return exitcode.Wrap(exitcode.Internal, err)
	}
	return nil
}

func newErrorDetail(err error, code exitcode.Code) errorDetail {
	detail := errorDetail{
		Code:     stableErrorCode(code),
		ExitCode: int(code),
		Message:  err.Error(),
	}

	var coded *exitcode.Error
	if errors.As(err, &coded) {
		detail.Hint = coded.Hint
	}

	var apiErr *play.APIError
	if errors.As(err, &apiErr) {
		detail.APICode = apiErr.Status
		detail.Reason = apiErr.Reason
	}

	return detail
}

func stableErrorCode(code exitcode.Code) string {
	switch code {
	case exitcode.OK:
		return "ok"
	case exitcode.Usage:
		return "usage"
	case exitcode.Auth:
		return "auth"
	case exitcode.NotFound:
		return "not_found"
	case exitcode.Conflict:
		return "conflict"
	case exitcode.Transient:
		return "transient"
	case exitcode.Rejected:
		return "rejected"
	case exitcode.ManualAction:
		return "manual_action"
	case exitcode.Interrupted:
		return "interrupted"
	case exitcode.Internal:
		return "internal"
	}
	return "internal"
}

func commandName(cmd *cobra.Command) string {
	if cmd == nil {
		return "google-play-publisher"
	}
	return cmd.Name()
}

func commandPackage(cmd *cobra.Command) string {
	if cmd == nil || cmd.Flags().Lookup("package") == nil {
		return ""
	}
	pkg, err := cmd.Flags().GetString("package")
	if err != nil {
		return ""
	}
	return pkg
}
