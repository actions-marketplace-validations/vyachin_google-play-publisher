package play

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

type partialBody struct {
	data   []byte
	pos    int
	err    error
	closed bool
}

func (b *partialBody) Read(p []byte) (int, error) {
	if b.pos < len(b.data) {
		n := copy(p, b.data[b.pos:])
		b.pos += n
		return n, nil
	}
	if b.err != nil {
		err := b.err
		b.err = nil
		return 0, err
	}
	return 0, io.EOF
}

func (b *partialBody) Close() error {
	b.closed = true
	return nil
}

type countingBody struct {
	payload []byte
	pos     int
	read    int
}

func (b *countingBody) Read(p []byte) (int, error) {
	if b.pos >= len(b.payload) {
		return 0, io.EOF
	}
	n := copy(p, b.payload[b.pos:])
	b.pos += n
	b.read += n
	return n, nil
}

func (b *countingBody) Close() error { return nil }

func TestCaptureBodyReplaysReadError(t *testing.T) {
	body := &partialBody{data: []byte(`{"id":"edit-1"`), err: io.ErrUnexpectedEOF}

	logged, replay := captureBody(body, 64, "application/json")
	if !strings.Contains(logged, "edit-1") {
		t.Fatalf("captured body = %q, want the read prefix", logged)
	}

	got, err := io.ReadAll(replay)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read error = %v, want io.ErrUnexpectedEOF", err)
	}
	if string(got) != string(body.data) {
		t.Fatalf("replayed %q, want %q", got, body.data)
	}

	if body.closed {
		t.Fatal("body must stay open while the caller reads the replay")
	}
	if err := replay.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !body.closed {
		t.Fatal("Close must close the original body")
	}
}

func TestCaptureBodyCapsBufferedBytes(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), maxLoggedBody*3)
	body := &countingBody{payload: payload}

	logged, replay := captureBody(body, int64(len(payload)), "application/json")

	if body.read > maxLoggedBody+1 {
		t.Fatalf("capture read %d bytes, want at most %d", body.read, maxLoggedBody+1)
	}
	if len(logged) > maxLoggedBody+len("...(truncated)") {
		t.Fatalf("log copy is %d bytes, want it truncated", len(logged))
	}

	got, err := io.ReadAll(replay)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("replayed body changed: %d bytes, want %d", len(got), len(payload))
	}
}

func TestCaptureBodyIgnoresSmallContentLengthHint(t *testing.T) {
	payload := bytes.Repeat([]byte("b"), maxLoggedBody*2)
	body := &countingBody{payload: payload}

	// The header claims a small body; capture must still stop at its budget.
	_, replay := captureBody(body, 16, "application/json")
	if body.read > maxLoggedBody+1 {
		t.Fatalf("capture read %d bytes, want at most %d", body.read, maxLoggedBody+1)
	}

	got, err := io.ReadAll(replay)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("replayed body changed: %d bytes, want %d", len(got), len(payload))
	}
}

func TestCaptureBodyClosesFullyCapturedBody(t *testing.T) {
	body := &partialBody{data: []byte(`{"id":"edit-1"}`)}

	_, replay := captureBody(body, 32, "application/json")
	if !body.closed {
		t.Fatal("a fully captured body must be closed immediately")
	}
	if _, err := io.ReadAll(replay); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
}

func TestCaptureBodyLeavesUnsupportedBodiesUntouched(t *testing.T) {
	tests := []struct {
		name          string
		contentLength int64
		contentType   string
	}{
		{"unknown length", -1, "application/json"},
		{"not json", 32, "application/octet-stream"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := &partialBody{data: []byte("payload")}
			logged, replay := captureBody(body, test.contentLength, test.contentType)
			if logged != "" {
				t.Fatalf("captured %q, want no capture", logged)
			}
			if replay != body {
				t.Fatal("the original body must be returned unchanged")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportPreservesPartialReadError(t *testing.T) {
	read := func(debug bool) ([]byte, error, bool) {
		body := &partialBody{data: []byte(`{"id":"edit-1"`), err: io.ErrUnexpectedEOF}
		base := roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Header:        http.Header{"Content-Type": {"application/json"}},
				Body:          body,
				ContentLength: 64,
			}, nil
		})

		tr := newTransport(base, transportOptions{Debug: debug})
		resp, err := tr.RoundTrip(&http.Request{
			Method: http.MethodGet,
			URL:    &url.URL{Scheme: "https", Host: "example.com", Path: "/"},
			Header: http.Header{},
		})
		if err != nil {
			t.Fatalf("RoundTrip: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		data, readErr := io.ReadAll(resp.Body)
		return data, readErr, body.closed
	}

	debugData, debugErr, debugClosed := read(true)
	plainData, plainErr, plainClosed := read(false)

	if !bytes.Equal(debugData, plainData) {
		t.Fatalf("debug changed the payload: %q vs %q", debugData, plainData)
	}
	if !errors.Is(debugErr, io.ErrUnexpectedEOF) || !errors.Is(plainErr, io.ErrUnexpectedEOF) {
		t.Fatalf("read errors differ: debug=%v plain=%v", debugErr, plainErr)
	}
	if debugClosed != plainClosed {
		t.Fatalf("close semantics differ: debug=%v plain=%v", debugClosed, plainClosed)
	}
}

func TestCaptureBodyRedactsLongSecret(t *testing.T) {
	token := strings.Repeat("s", 5000)
	raw := []byte(`{"access_token":"` + token + `"}`)
	body := &countingBody{payload: raw}

	logged, replay := captureBody(body, int64(len(raw)), "application/json")
	if strings.Contains(logged, strings.Repeat("s", 20)) {
		t.Fatalf("debug copy leaks the token prefix: %q", logged)
	}
	if !strings.Contains(logged, `"access_token":"[redacted]"`) {
		t.Fatalf("token was not redacted: %q", logged)
	}

	got, err := io.ReadAll(replay)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("replayed body changed: %d bytes, want %d", len(got), len(raw))
	}
}

func TestCaptureBodyRedactsSecretNearCaptureLimit(t *testing.T) {
	token := strings.Repeat("s", 5000)
	raw := []byte(`{"pad":"` + strings.Repeat("x", 4050) + `","access_token":"` + token + `"}`)
	body := &countingBody{payload: raw}

	logged, _ := captureBody(body, int64(len(raw)), "application/json")
	if strings.Contains(logged, strings.Repeat("s", 20)) {
		t.Fatalf("debug copy leaks a token that starts before the limit: %q", logged)
	}
	if !strings.Contains(logged, `"access_token":"[redacted]"`) {
		t.Fatalf("token was not redacted: %q", logged)
	}
}

func TestCaptureBodyRedactsEscapedSecret(t *testing.T) {
	raw := []byte(`{"client_secret":"abc\"def","id":"edit-1"}`)

	logged := redact(raw, false)
	if strings.Contains(logged, "abc") || strings.Contains(logged, "def") {
		t.Fatalf("escaped secret leaked: %q", logged)
	}
	if !strings.Contains(logged, `"client_secret":"[redacted]"`) {
		t.Fatalf("escaped secret was not redacted: %q", logged)
	}
	if !strings.Contains(logged, `"id":"edit-1"`) {
		t.Fatalf("non-secret content changed: %q", logged)
	}
}

func TestCaptureBodyKeepsEscapedNonSecret(t *testing.T) {
	raw := []byte(`{"note":"a\"b"}`)

	logged := redact(raw, false)
	if logged != string(raw) {
		t.Fatalf("logged body = %q, want %q", logged, raw)
	}
}

func TestCaptureBodyRedactsSecretOnPartialRead(t *testing.T) {
	body := &partialBody{
		data: []byte(`{"refresh_token":"secret-value`),
		err:  io.ErrUnexpectedEOF,
	}

	logged, _ := captureBody(body, 64, "application/json")
	if strings.Contains(logged, "secret-value") {
		t.Fatalf("partial read leaked the token: %q", logged)
	}
	if !strings.Contains(logged, `"refresh_token":"[redacted]"`) {
		t.Fatalf("partial-read token was not redacted: %q", logged)
	}
}

func TestRedactOmitsUnredactableBody(t *testing.T) {
	if logged := redact([]byte(`{"access_token":123}`), false); logged != "" {
		t.Fatalf("unsafe body was logged: %q", logged)
	}
	if logged := redact([]byte(`{"access_token":null}`), false); logged != "" {
		t.Fatalf("unsafe body was logged: %q", logged)
	}
}

func TestRedactHandlesMultipleSecrets(t *testing.T) {
	raw := []byte(`{"access_token":"one","id":"edit-1","refresh_token":"two"}`)

	logged := redact(raw, false)
	for _, secret := range []string{"one", "two"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("secret %q leaked: %q", secret, logged)
		}
	}
	if got := strings.Count(logged, "[redacted]"); got != 2 {
		t.Fatalf("redacted %d values, want 2: %q", got, logged)
	}
}

func TestRedactTruncatesAtRuneBoundary(t *testing.T) {
	payload := []byte(strings.Repeat("a", maxLoggedBody-1) + "€")

	logged := redact(payload, false)
	if !utf8.ValidString(logged) {
		t.Fatalf("truncated log is not valid UTF-8: %q", logged)
	}
	if !strings.HasSuffix(logged, "...(truncated)") {
		t.Fatalf("log copy is not marked as truncated: %q", logged)
	}
}
