package play

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// maxLoggedBody limits how much of an HTTP body is written to the debug log.
const maxLoggedBody = 4 << 10

type transportOptions struct {
	Logger    *slog.Logger
	Debug     bool
	UserAgent string
}

// transport adds a User-Agent header and, in debug mode, logs HTTP exchanges.
type transport struct {
	base http.RoundTripper
	opts transportOptions
}

func newTransport(base http.RoundTripper, opts transportOptions) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &transport{base: base, opts: opts}
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.opts.UserAgent != "" {
		req.Header.Set("User-Agent", t.opts.UserAgent)
	}
	if !t.opts.Debug {
		return t.base.RoundTrip(req)
	}

	start := time.Now()
	var reqBody string
	reqBody, req.Body = captureBody(req.Body, req.ContentLength, req.Header.Get("Content-Type"))

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		t.opts.Logger.Debug("http request failed",
			"method", req.Method,
			"url", req.URL.String(),
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err,
		)
		return nil, err
	}

	attrs := []any{
		"method", req.Method,
		"url", req.URL.String(),
		"status", resp.StatusCode,
		"duration_ms", time.Since(start).Milliseconds(),
		"request_id", requestID(resp),
	}
	if reqBody != "" {
		attrs = append(attrs, "request_body", reqBody)
	}

	var respBody string
	respBody, resp.Body = captureBody(resp.Body, resp.ContentLength, resp.Header.Get("Content-Type"))
	if respBody != "" {
		attrs = append(attrs, "response_body", respBody)
	}

	t.opts.Logger.Debug("http response", attrs...)
	return resp, nil
}

// captureBody reads a JSON body, returns a redacted copy and a replacement
// reader. Unknown-length and non-JSON bodies are left untouched.
//
// The replacement always yields the same bytes and the same read error as the
// original body, so debug mode never changes a payload. Content-Length is only
// a hint: the amount buffered is capped by io.LimitReader.
func captureBody(body io.ReadCloser, contentLength int64, contentType string) (string, io.ReadCloser) {
	if body == nil || contentLength < 0 || !strings.Contains(contentType, "json") {
		return "", body
	}

	data, err := io.ReadAll(io.LimitReader(body, maxLoggedBody+1))
	if err != nil {
		// Replay the consumed bytes, then the original error.
		return redact(data), &replayReadCloser{
			reader: io.MultiReader(bytes.NewReader(data), errorReader{err}),
			closer: body,
		}
	}
	if len(data) > maxLoggedBody {
		// The body is larger than the log budget: replay the captured prefix
		// and continue reading the rest from the original body.
		return redact(data), &replayReadCloser{
			reader: io.MultiReader(bytes.NewReader(data), body),
			closer: body,
		}
	}

	_ = body.Close()
	return redact(data), io.NopCloser(bytes.NewReader(data))
}

// replayReadCloser replays captured bytes and closes the original body.
type replayReadCloser struct {
	reader io.Reader
	closer io.Closer
}

func (r *replayReadCloser) Read(p []byte) (int, error) { return r.reader.Read(p) }

func (r *replayReadCloser) Close() error { return r.closer.Close() }

// errorReader reports a read error that happened while capturing the body.
type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func requestID(resp *http.Response) string {
	for _, header := range []string{"X-Goog-Request-Id", "X-Request-Id", "X-Guploader-Uploadid"} {
		if v := resp.Header.Get(header); v != "" {
			return v
		}
	}
	return ""
}

var secretJSON = regexp.MustCompile(`"(private_key|client_secret|refresh_token|access_token)"\s*:\s*"[^"]*"`)

func redact(data []byte) string {
	s := secretJSON.ReplaceAllString(string(data), `"$1":"[redacted]"`)
	if len(s) > maxLoggedBody {
		return s[:maxLoggedBody] + "...(truncated)"
	}
	return s
}
