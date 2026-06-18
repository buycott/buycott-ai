package llm

import (
	"context"
	"log"
	"math/rand"
	"strings"
	"time"
)

const (
	rlBaseDelay = 5 * time.Second
	rlMaxDelay  = 5 * time.Minute
	// rlDefaultMaxWait is how long a single call will keep retrying a rate limit
	// before giving up. It's long enough to ride out a full rolling-window
	// cooldown (e.g. a subscription's ~5-hour limit) unattended, so a pause
	// doesn't derail the run. Configurable via execution.rate_limit_max_wait.
	rlDefaultMaxWait = 6 * time.Hour
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
		strings.Contains(msg, "ratelimitexceeded") ||
		// Subscription-CLI limit phrasings (claude-code / codex / gemini-cli).
		// Lean inclusive: a missed match derails the run; a false match only
		// makes a non-rate-limit error wait out max_wait before failing. Verify
		// against the real CLI output and tune if a version uses other wording.
		strings.Contains(msg, "usage limit") ||
		strings.Contains(msg, "limit reached") ||
		strings.Contains(msg, "weekly limit") ||
		strings.Contains(msg, "session limit")
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
		var n float64
		for _, ch := range rest[:numEnd] {
			if ch == '.' {
				continue // whole seconds/minutes are fine here; drop the fraction
			}
			n = n*10 + float64(ch-'0')
		}
		if n <= 0 {
			continue
		}
		// Honor the unit that follows the number, if any ("30s", "5 min", "2 hours").
		unit := strings.TrimSpace(rest[numEnd:])
		mult := time.Second
		switch {
		case strings.HasPrefix(unit, "ms"):
			mult = time.Millisecond
		case strings.HasPrefix(unit, "h"):
			mult = time.Hour
		case strings.HasPrefix(unit, "m"): // minute(s); "ms" is handled above
			mult = time.Minute
		}
		d := time.Duration(n) * mult
		if d > 24*time.Hour {
			d = 24 * time.Hour // sanity cap; the retry loop further bounds this by max_wait
		}
		return d + time.Second // +1s safety margin
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
	maxWait  time.Duration // total time to ride out a rate limit before giving up
}

// RetryOption configures a RetryingProvider.
type RetryOption func(*RetryingProvider)

// WithMaxWait sets how long a single call keeps retrying a rate-limited error
// before giving up — long enough to ride out a rolling-window cooldown so a
// pause doesn't derail the run. A non-positive value is ignored (keeps default).
func WithMaxWait(d time.Duration) RetryOption {
	return func(p *RetryingProvider) {
		if d > 0 {
			p.maxWait = d
		}
	}
}

func NewRetryingProvider(inner Provider, roleName string, fn RateLimitFunc, opts ...RetryOption) Provider {
	p := &RetryingProvider{inner: inner, roleName: roleName, onRL: fn, maxWait: rlDefaultMaxWait}
	for _, o := range opts {
		o(p)
	}
	return p
}

func (p *RetryingProvider) Name() string { return p.inner.Name() }

func (p *RetryingProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	var waited time.Duration
	for attempt := 0; ; attempt++ {
		resp, err := p.inner.Complete(ctx, req)
		if err == nil {
			if attempt > 0 && p.onRL != nil {
				p.onRL(p.roleName, time.Time{}, -1) // signal cleared
			}
			return resp, nil
		}

		isRL, retryAfter := isRateLimitErr(err)
		if !isRL || waited >= p.maxWait {
			return CompletionResponse{}, err
		}

		delay := rlBackoff(attempt, retryAfter)
		retryAt := time.Now().Add(delay)
		if p.onRL != nil {
			p.onRL(p.roleName, retryAt, attempt+1)
		}
		log.Printf("[rate-limit] %s: paused, retry %d in %s (waited %s/%s)",
			p.roleName, attempt+1, delay.Round(time.Second), waited.Round(time.Second), p.maxWait)

		select {
		case <-ctx.Done():
			return CompletionResponse{}, ctx.Err()
		case <-time.After(delay):
		}
		waited += delay
	}
}

// Stream retries on rate-limit errors that occur before any tokens are sent.
// This is the common case — limits typically reject at connection time, not
// mid-stream. If the inner Stream wrote tokens to ch before failing, retrying
// would produce duplicate output; we accept this as an unlikely edge case.
func (p *RetryingProvider) Stream(ctx context.Context, req CompletionRequest, ch chan<- string) error {
	var waited time.Duration
	for attempt := 0; ; attempt++ {
		err := p.inner.Stream(ctx, req, ch)
		if err == nil {
			if attempt > 0 && p.onRL != nil {
				p.onRL(p.roleName, time.Time{}, -1)
			}
			return nil
		}

		isRL, retryAfter := isRateLimitErr(err)
		if !isRL || waited >= p.maxWait {
			return err
		}

		delay := rlBackoff(attempt, retryAfter)
		retryAt := time.Now().Add(delay)
		if p.onRL != nil {
			p.onRL(p.roleName, retryAt, attempt+1)
		}
		log.Printf("[rate-limit] %s (stream): paused, retry %d in %s (waited %s/%s)",
			p.roleName, attempt+1, delay.Round(time.Second), waited.Round(time.Second), p.maxWait)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		waited += delay
	}
}
