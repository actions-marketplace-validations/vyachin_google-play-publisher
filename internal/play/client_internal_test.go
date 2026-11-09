package play

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"cloud.google.com/go/auth"
)

// blockingTokenProvider blocks until the context it receives is done. It is
// used to prove that token acquisition sees the operation context.
type blockingTokenProvider struct {
	calls int
}

func (p *blockingTokenProvider) Token(ctx context.Context) (*auth.Token, error) {
	p.calls++
	<-ctx.Done()
	return nil, ctx.Err()
}

// expiredTokenProvider returns an expired token once and then blocks, which
// forces the cached provider into the blocking refresh path.
type expiredTokenProvider struct {
	calls int
}

func (p *expiredTokenProvider) Token(ctx context.Context) (*auth.Token, error) {
	p.calls++
	if p.calls == 1 {
		return &auth.Token{Value: "expired", Type: "Bearer", Expiry: time.Now().Add(-time.Hour)}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func testAuthClient(t *testing.T, provider auth.TokenProvider) *http.Client {
	t.Helper()

	creds := auth.NewCredentials(&auth.CredentialsOptions{TokenProvider: provider})
	client, err := authenticatedClient(creds)
	if err != nil {
		t.Fatalf("authenticatedClient: %v", err)
	}
	return client
}

func doWithTimeout(t *testing.T, client *http.Client, timeout time.Duration) (time.Duration, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:1/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	start := time.Now()
	resp, err := client.Do(req)
	closeBody(resp)
	return time.Since(start), err
}

func closeBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func TestTokenAcquisitionHonorsDeadline(t *testing.T) {
	provider := &blockingTokenProvider{}
	client := testAuthClient(t, provider)

	elapsed, err := doWithTimeout(t, client, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > time.Second {
		t.Fatalf("token acquisition ignored the deadline: %s", elapsed)
	}
	if provider.calls != 1 {
		t.Fatalf("token calls = %d, want 1", provider.calls)
	}
}

func TestTokenAcquisitionHonorsCancellation(t *testing.T) {
	provider := &blockingTokenProvider{}
	client := testAuthClient(t, provider)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:1/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	resp, err := client.Do(req)
	closeBody(resp)
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if elapsed > time.Second {
		t.Fatalf("token acquisition ignored cancellation: %s", elapsed)
	}
}

func TestTokenRefreshHonorsDeadline(t *testing.T) {
	provider := &expiredTokenProvider{}
	client := testAuthClient(t, provider)

	// The first token is expired; the request fails at the network layer, but
	// the provider call still happens.
	firstCtx, firstCancel := context.WithTimeout(context.Background(), time.Second)
	defer firstCancel()
	firstReq, err := http.NewRequestWithContext(firstCtx, http.MethodGet, "http://127.0.0.1:1/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, _ := client.Do(firstReq)
	closeBody(resp)

	elapsed, err := doWithTimeout(t, client, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("refresh error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > time.Second {
		t.Fatalf("token refresh ignored the deadline: %s", elapsed)
	}
	if provider.calls < 2 {
		t.Fatalf("provider calls = %d, want a refresh attempt", provider.calls)
	}
}
