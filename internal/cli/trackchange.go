package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

// trackChangeJSON is the shared JSON result of rollout and promote. Both
// commands change a track without uploading a bundle, so they report the same
// fields; promote additionally fills From.
type trackChangeJSON struct {
	SchemaVersion int             `json:"schemaVersion"`
	OK            bool            `json:"ok"`
	Command       string          `json:"command"`
	Package       string          `json:"package"`
	From          string          `json:"from,omitempty"`
	Track         string          `json:"track"`
	Status        string          `json:"status"`
	VersionCodes  []int64         `json:"versionCodes"`
	UserFraction  float64         `json:"userFraction,omitempty"`
	EditID        string          `json:"editId,omitempty"`
	Review        string          `json:"review"`
	Metadata      *metadataOutput `json:"metadata,omitempty"`
	DurationMs    int64           `json:"durationMs"`
}

func writeTrackChangeJSON(w io.Writer, out trackChangeJSON, duration time.Duration) error {
	out.SchemaVersion = 1
	out.OK = true
	out.DurationMs = duration.Milliseconds()

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return exitcode.Wrap(exitcode.Internal, err)
	}
	return nil
}

// reportSavedTrackChange writes the result of a change that was saved without
// being submitted for review, then keeps exit code 8. In JSON mode the success
// object stays the only object on stdout.
func reportSavedTrackChange(
	a *App,
	g *globalFlags,
	out trackChangeJSON,
	text string,
	duration time.Duration,
	err error,
) error {
	if g.JSON {
		if writeErr := writeTrackChangeJSON(a.Stdout, out, duration); writeErr != nil {
			return writeErr
		}
		return &renderedError{err: err}
	}

	if _, printErr := fmt.Fprintln(a.Stdout, text); printErr != nil {
		// The change is already saved, so keep the manual-action code; the
		// operator still has to submit it even if the result was not written.
		return exitcode.WithHint(
			exitcode.Wrap(exitcode.ManualAction,
				fmt.Errorf("%w; writing the result failed: %v", err, printErr)),
			"open Play Console -> Publishing overview and send the changes for review")
	}
	return err
}

// formatVersionCodes renders version codes for one-line results.
func formatVersionCodes(codes []int64) string {
	if len(codes) == 0 {
		return "unknown"
	}
	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		parts = append(parts, strconv.FormatInt(code, 10))
	}
	return strings.Join(parts, ",")
}

// formatPercent renders a rollout fraction, rounded to two decimal places so
// binary floating point noise never reaches the output.
func formatPercent(fraction float64) string {
	percent := math.Round(fraction*10000) / 100
	return strconv.FormatFloat(percent, 'f', -1, 64) + "%"
}
