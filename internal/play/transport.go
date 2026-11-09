package play

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
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
		return redact(data, true), &replayReadCloser{
			reader: io.MultiReader(bytes.NewReader(data), errorReader{err}),
			closer: body,
		}
	}
	if len(data) > maxLoggedBody {
		// The body is larger than the log budget: replay the captured prefix
		// and continue reading the rest from the original body.
		return redact(data, true), &replayReadCloser{
			reader: io.MultiReader(bytes.NewReader(data), body),
			closer: body,
		}
	}

	_ = body.Close()
	return redact(data, false), io.NopCloser(bytes.NewReader(data))
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

// secretKeys are JSON fields whose values must never reach a log.
var secretKeys = map[string]bool{
	"private_key":   true,
	"client_secret": true,
	"refresh_token": true,
	"access_token":  true,
}

// redact returns a log-safe copy of a possibly truncated JSON body. Values of
// secret keys are replaced with [redacted], including values cut off by the
// capture limit and values containing escaped quotes. When safe redaction
// cannot be established, it returns an empty string and the caller logs no
// body at all.
func redact(data []byte, truncated bool) string {
	out, safe := redactJSON(data)
	if !safe {
		return ""
	}
	if len(out) > maxLoggedBody {
		out = truncateUTF8(out, maxLoggedBody)
		truncated = true
	}
	if truncated {
		out += "...(truncated)"
	}
	return out
}

// redactJSON scans JSON string tokens instead of matching secret keys with a
// regular expression, so escaped quotes and a truncated value are handled
// correctly. It reports false when a secret key is followed by something that
// cannot be redacted safely, for example an unquoted value.
func redactJSON(data []byte) (string, bool) {
	var out bytes.Buffer
	out.Grow(len(data))

	awaitSecret := false
	i := 0
	for i < len(data) {
		if data[i] != '"' {
			if awaitSecret {
				if c := data[i]; !isJSONSpace(c) && c != ':' {
					return "", false
				}
			}
			out.WriteByte(data[i])
			i++
			continue
		}

		end, closed := scanJSONString(data, i)
		if !closed {
			if awaitSecret {
				out.WriteString(`"[redacted]"`)
			} else {
				out.Write(data[i:])
			}
			return out.String(), true
		}

		if awaitSecret {
			out.WriteString(`"[redacted]"`)
			awaitSecret = false
			i = end
			continue
		}

		out.Write(data[i:end])
		// A closed string is a key when the next non-space byte is a colon.
		j := end
		for j < len(data) && isJSONSpace(data[j]) {
			j++
		}
		if j < len(data) && data[j] == ':' {
			var key string
			if err := json.Unmarshal(data[i:end], &key); err != nil {
				// The key cannot be classified, so the body cannot be
				// redacted safely.
				return "", false
			}
			if secretKeys[key] {
				awaitSecret = true
			}
		}
		i = end
	}
	return out.String(), true
}

// scanJSONString returns the index just past the closing quote of the JSON
// string starting at i. The second result is false when the string is cut off.
func scanJSONString(data []byte, i int) (int, bool) {
	j := i + 1
	for j < len(data) {
		switch data[j] {
		case '\\':
			j += 2
		case '"':
			return j + 1, true
		default:
			j++
		}
	}
	return len(data), false
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}
