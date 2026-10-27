package gateway

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Grok Build reports these error kinds for transient throttling. They are
// request-rate signals, not credential failures: cool the account briefly,
// switch accounts, never mark reauth.
var buildRateLimitCodes = []string{"rate_limited", "concurrency_limit", "global_rate_limit"}

// Grok Build quota kinds that must use the long quota-block path.
var buildUsageExhaustedCodes = []string{"usage_pool_exhausted", "usage_limit_reached"}

const (
	DefaultRateLimitMinCooldown     = 5 * time.Second
	DefaultRateLimitMaxCooldown     = 120 * time.Second
	DefaultRateLimitDefaultCooldown = 10 * time.Second
	DefaultQueueMaxWait             = 30 * time.Second
	DefaultQueueMaxLength           = 256
	queueTimeoutRetryAfter          = 5 * time.Second
)

// RateLimitConfig bounds the short per-account cooldown applied to Build 429s.
type RateLimitConfig struct {
	MinCooldown     time.Duration
	MaxCooldown     time.Duration
	DefaultCooldown time.Duration
}

// QueueConfig controls the in-process wait when every eligible account is
// saturated or briefly rate-limited. MaxWait 0 disables queueing.
type QueueConfig struct {
	MaxWait   time.Duration
	MaxLength int
}

var queuedRequests atomic.Int64

// QueuedRequests reports how many gateway requests are currently waiting for an account.
func QueuedRequests() int64 { return queuedRequests.Load() }

func (s *Service) UpdateRateLimit(value RateLimitConfig) {
	if value.MinCooldown <= 0 {
		value.MinCooldown = DefaultRateLimitMinCooldown
	}
	if value.MaxCooldown <= 0 {
		value.MaxCooldown = DefaultRateLimitMaxCooldown
	}
	if value.MaxCooldown < value.MinCooldown {
		value.MaxCooldown = value.MinCooldown
	}
	if value.DefaultCooldown <= 0 {
		value.DefaultCooldown = DefaultRateLimitDefaultCooldown
	}
	s.rateLimit.Store(&value)
}

func (s *Service) rateLimitConfig() RateLimitConfig {
	if value := s.rateLimit.Load(); value != nil {
		return *value
	}
	return RateLimitConfig{MinCooldown: DefaultRateLimitMinCooldown, MaxCooldown: DefaultRateLimitMaxCooldown, DefaultCooldown: DefaultRateLimitDefaultCooldown}
}

func (s *Service) UpdateQueue(value QueueConfig) {
	value.MaxWait = max(0, value.MaxWait)
	if value.MaxLength <= 0 {
		value.MaxLength = DefaultQueueMaxLength
	}
	s.queue.Store(&value)
}

func (s *Service) queueConfig() QueueConfig {
	if value := s.queue.Load(); value != nil {
		return *value
	}
	return QueueConfig{MaxWait: DefaultQueueMaxWait, MaxLength: DefaultQueueMaxLength}
}

// cooldown maps an upstream Retry-After to the bounded account cooldown.
func (c RateLimitConfig) cooldown(retryAfter time.Duration) time.Duration {
	value := retryAfter
	if value <= 0 {
		value = c.DefaultCooldown
	}
	if value < c.MinCooldown {
		value = c.MinCooldown
	}
	if c.MaxCooldown > 0 && value > c.MaxCooldown {
		value = c.MaxCooldown
	}
	return value
}

func bodyHasErrorKind(failure *UpstreamFailure, body []byte, kinds []string) bool {
	code := ""
	if failure != nil {
		code = strings.ToLower(strings.TrimSpace(failure.UpstreamCode))
	}
	text := strings.ToLower(string(body))
	for _, kind := range kinds {
		if code == kind || strings.Contains(text, `"`+kind+`"`) {
			return true
		}
	}
	return false
}

// isBuildRateLimitFailure: 429 or an explicit throttling kind, excluding every
// quota-exhaustion signal (those keep their long quota-block recovery).
func isBuildRateLimitFailure(status int, failure *UpstreamFailure, body []byte) bool {
	if failure == nil || failure.QuotaExhausted || failure.FreeQuotaExhausted || failure.ModelQuotaExhausted || failure.SpendingLimitBlocked || failure.AccountBlocked {
		return false
	}
	return status == http.StatusTooManyRequests || bodyHasErrorKind(failure, body, buildRateLimitCodes)
}

// queueAdmission decides whether a failed selection should wait in the queue.
// It returns the wait hint (cooldown expiry) or false when the request must fail now.
func queueAdmission(err error, lastFailure *UpstreamFailure, remaining time.Duration) (time.Duration, bool) {
	if remaining <= 0 {
		return 0, false
	}
	var selectionErr *SelectionUnavailableError
	if !errors.As(err, &selectionErr) {
		return 0, false
	}
	switch selectionErr.Reason {
	case SelectionSaturated:
		return 0, true
	case SelectionCooling, SelectionModelCooling:
		// Only a short cooldown on a fresh request: if this request already hit
		// failures on the remaining accounts, return them instead of waiting.
		if lastFailure == nil && selectionErr.RetryAfter > 0 && selectionErr.RetryAfter <= remaining {
			return selectionErr.RetryAfter, true
		}
	}
	return 0, false
}

// awaitQueue parks the request until a lease is released, the hinted cooldown
// expires, the deadline passes, or the client cancels. It returns false when
// the queue is full or the deadline has already passed.
func (s *Service) awaitQueue(ctx context.Context, deadline time.Time, hint time.Duration, maxLength int) (bool, error) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false, nil
	}
	if count := queuedRequests.Add(1); maxLength > 0 && count > int64(maxLength) {
		queuedRequests.Add(-1)
		return false, nil
	}
	defer queuedRequests.Add(-1)
	wait := remaining
	if hint > 0 && hint < wait {
		wait = hint
	}
	var notice <-chan struct{}
	if s.selector != nil {
		notice = s.selector.leaseReturnNotice()
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-notice:
	case <-timer.C:
	}
	return true, nil
}

var sensitiveHeaderFragments = []string{"token", "auth", "cookie", "secret", "key", "signature", "credential", "password", "session-token"}

func isSensitiveHeaderName(name string) bool {
	for _, fragment := range sensitiveHeaderFragments {
		if strings.Contains(name, fragment) {
			return true
		}
	}
	return false
}

// upstreamHeaderLog returns retry-after and every x-* header for logging, with
// secret-looking names redacted.
func upstreamHeaderLog(headers http.Header) map[string]string {
	result := map[string]string{}
	for name, values := range headers {
		lower := strings.ToLower(name)
		if lower != "retry-after" && !strings.HasPrefix(lower, "x-") && !strings.HasPrefix(lower, "ratelimit") {
			continue
		}
		if isSensitiveHeaderName(lower) {
			result[lower] = "[REDACTED]"
			continue
		}
		result[lower] = sanitizeDiagnosticText(strings.Join(values, ", "), diagnosticHeaderValueLimit)
	}
	return result
}
