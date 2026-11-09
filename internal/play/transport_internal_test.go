package play

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
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
