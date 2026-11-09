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
// reader. Large, non-JSON and unknown-length bodies are left untouched.
func captureBody(body io.ReadCloser, contentLength int64, contentType string) (string, io.ReadCloser) {
	if body == nil || contentLength < 0 || contentLength > maxLoggedBody {
		return "", body
	}
	if !strings.Contains(contentType, "json") {
		return "", body
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return "", body
	}
	_ = body.Close()

	return redact(data), io.NopCloser(bytes.NewReader(data))
}

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
