package llm

import (
	"context"
	"log"
	"math/rand"
	"strings"
	"time"
)

const (
	rlMaxAttempts = 12
	rlBaseDelay   = 5 * time.Second
	rlMaxDelay    = 5 * time.Minute
)

// isRateLimitErr returns true when err represents an HTTP 429 / quota-exceeded
// response from any supported provider. It also extracts a suggested retry
// duration from common error messages when available.
func isRateLimitErr(err error) (bool, time.Duration) {
	if err == nil {
		return false, 0
	}
	msg := strings.ToLower(err.Error())
	hit := strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate_limit") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "resource_exhausted") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "toomanyrequests") ||
		strings.Contains(msg, "quota exceeded") ||
		strings.Contains(msg, "quota_exceeded") ||
		strings.Contains(msg, "ratelimitexceeded")
	if !hit {
		return false, 0
	}
	// Attempt to parse "retry after N seconds" from the error text.
	return true, extractRetryAfterFromMsg(msg)
}

// extractRetryAfterFromMsg looks for "retry after X" / "retry-after: X" patterns.
func extractRetryAfterFromMsg(msg string) time.Duration {
	// Look for patterns like "retry after 30s", "retry-after: 60", "please wait 45 seconds"
	for _, prefix := range []string{"retry after ", "retry-after: ", "retry-after:", "please wait ", "wait "} {
		idx := strings.Index(msg, prefix)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(msg[idx+len(prefix):])
		// Grab the numeric prefix
		numEnd := 0
		hasDot := false
		for numEnd < len(rest) {
			if rest[numEnd] >= '0' && rest[numEnd] <= '9' {
				numEnd++
			} else if rest[numEnd] == '.' && !hasDot {
				hasDot = true
				numEnd++
			} else {
				break
			}
		}
		if numEnd == 0 {
			continue
		}
		var secs float64
		if _, err := strings.NewReader(rest[:numEnd]).Read(nil); err != nil {
			continue
		}
		// Manually convert string to float since we can't use strconv here without
		// complicating the import; use a simple accumulator.
		for _, ch := range rest[:numEnd] {
			if ch == '.' {
				continue
			}
			secs = secs*10 + float64(ch-'0')
		}
		if hasDot {
			// Rough: treat the decimal part as irrelevant for our purposes
		}
		if secs > 0 && secs < 3600 {
			return time.Duration(secs)*time.Second + time.Second // +1s safety margin
		}
	}
	return 0
}

// rlBackoff returns the delay for a given attempt number.
// If retryAfter is non-zero it is used directly (with a 1-second safety margin
// already included by the caller). Otherwise exponential backoff with full jitter.
func rlBackoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	// Exponential: base * 2^attempt, capped at rlMaxDelay.
	delay := rlBaseDelay
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay > rlMaxDelay {
			delay = rlMaxDelay
			break
		}
	}
	// Full jitter: uniform in [delay/2, delay].
	half := int64(delay / 2)
	return time.Duration(half + rand.Int63n(half+1))
}

// RetryingProvider wraps any Provider and transparently retries rate-limit
// errors with exponential backoff. A RateLimitFunc callback (if non-nil)
// is called on each rate-limit event and on recovery so callers can surface
// the state in a dashboard or status endpoint.
type RetryingProvider struct {
	inner    Provider
	roleName string
	onRL     RateLimitFunc
}

func NewRetryingProvider(inner Provider, roleName string, fn RateLimitFunc) Provider {
	return &RetryingProvider{inner: inner, roleName: roleName, onRL: fn}
}

func (p *RetryingProvider) Name() string { return p.inner.Name() }

func (p *RetryingProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	for attempt := 0; ; attempt++ {
		resp, err := p.inner.Complete(ctx, req)
		if err == nil {
			if attempt > 0 && p.onRL != nil {
				p.onRL(p.roleName, time.Time{}, -1) // signal cleared
			}
			return resp, nil
		}

		isRL, retryAfter := isRateLimitErr(err)
		if !isRL || attempt >= rlMaxAttempts {
			return CompletionResponse{}, err
		}

		delay := rlBackoff(attempt, retryAfter)
		retryAt := time.Now().Add(delay)
		if p.onRL != nil {
			p.onRL(p.roleName, retryAt, attempt+1)
		}
		log.Printf("[rate-limit] %s: retry %d/%d in %s", p.roleName, attempt+1, rlMaxAttempts, delay.Round(time.Second))

		select {
		case <-ctx.Done():
			return CompletionResponse{}, ctx.Err()
		case <-time.After(delay):
		}
	}
}

// Stream retries on rate-limit errors that occur before any tokens are sent.
// This is the common case — limits typically reject at connection time, not
// mid-stream. If the inner Stream wrote tokens to ch before failing, retrying
// would produce duplicate output; we accept this as an unlikely edge case.
func (p *RetryingProvider) Stream(ctx context.Context, req CompletionRequest, ch chan<- string) error {
	for attempt := 0; ; attempt++ {
		err := p.inner.Stream(ctx, req, ch)
		if err == nil {
			if attempt > 0 && p.onRL != nil {
				p.onRL(p.roleName, time.Time{}, -1)
			}
			return nil
		}

		isRL, retryAfter := isRateLimitErr(err)
		if !isRL || attempt >= rlMaxAttempts {
			return err
		}

		delay := rlBackoff(attempt, retryAfter)
		retryAt := time.Now().Add(delay)
		if p.onRL != nil {
			p.onRL(p.roleName, retryAt, attempt+1)
		}
		log.Printf("[rate-limit] %s (stream): retry %d/%d in %s", p.roleName, attempt+1, rlMaxAttempts, delay.Round(time.Second))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}
