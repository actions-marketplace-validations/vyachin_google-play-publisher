package play

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/googleapi"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

const (
	defaultRetryAttempts = 5
	defaultRetryBase     = time.Second
	defaultRetryCap      = 32 * time.Second
	maxRetryAfter        = 2 * time.Minute
)

// retryConfig bounds retries for safely repeatable API calls.
type retryConfig struct {
	attempts int
	base     time.Duration
	cap      time.Duration
	sleep    func(context.Context, time.Duration) error
}

func defaultRetryConfig() retryConfig {
	return retryConfig{
		attempts: defaultRetryAttempts,
		base:     defaultRetryBase,
		cap:      defaultRetryCap,
		sleep:    sleepContext,
	}
}

// call runs a safely repeatable API operation with bounded retries for
// transient failures. It honors Retry-After, stops at the configured attempt
// limit and never retries after the context is done.
func (c *Client) call(ctx context.Context, op string, attempts int, fn func(context.Context) error) error {
	if attempts <= 0 {
		attempts = 1
	}

	sleep := c.retry.sleep
	if sleep == nil {
		sleep = sleepContext
	}

	var (
		lastMapped error
		lastRaw    error
	)

	for attempt := 1; attempt <= attempts; attempt++ {
		err := fn(ctx)
		if err == nil {
			return nil
		}

		mapped := mapError(err)
		if exitcode.From(mapped) != exitcode.Transient || ctx.Err() != nil {
			return mapped
		}

		lastMapped, lastRaw = mapped, err
		if attempt == attempts {
			break
		}

		delay := c.retryDelay(attempt-1, lastRaw)
		c.logger.Debug("retrying request",
			"operation", op,
			"attempt", attempt,
			"delay_ms", delay.Milliseconds(),
			"error", err,
		)
		if sleepErr := sleep(ctx, delay); sleepErr != nil {
			return mapError(sleepErr)
		}
	}

	var coded *exitcode.Error
	if errors.As(lastMapped, &coded) {
		return &exitcode.Error{
			Code: coded.Code,
			Err:  fmt.Errorf("%w (after %d attempts)", coded.Err, attempts),
			Hint: coded.Hint,
		}
	}
	return lastMapped
}

// retryDelay returns Retry-After when Play sent one, otherwise an exponential
// backoff with jitter: min(cap, base * 2^retry) + rand(0..1s).
func (c *Client) retryDelay(retry int, err error) time.Duration {
	if delay, ok := retryAfter(err); ok {
		if delay > maxRetryAfter {
			return maxRetryAfter
		}
		return delay
	}

	delay := c.retry.base
	for i := 0; i < retry && delay < c.retry.cap; i++ {
		delay *= 2
	}
	if delay > c.retry.cap {
		delay = c.retry.cap
	}
	return delay + time.Duration(rand.Int64N(int64(time.Second)))
}

func retryAfter(err error) (time.Duration, bool) {
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) || gerr.Header == nil {
		return 0, false
	}

	raw := strings.TrimSpace(gerr.Header.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	if seconds, convErr := strconv.Atoi(raw); convErr == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	if when, parseErr := http.ParseTime(raw); parseErr == nil {
		delay := time.Until(when)
		if delay < 0 {
			delay = 0
		}
		return delay, true
	}
	return 0, false
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
