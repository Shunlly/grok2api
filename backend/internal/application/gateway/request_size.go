package gateway

import (
	"fmt"
	"net/http"
	"time"
)

// RequestTooLargeCode is the client/audit error code for bodies rejected by the upstream size guard.
const RequestTooLargeCode = "request_too_large"

// DefaultMaxUpstreamRequestBytes is the default generation body guard (2.5 MiB).
const DefaultMaxUpstreamRequestBytes int64 = 5 << 19

// Requests at or above this size never put accounts into cooldown/reauth on
// slow edge failures, because the failure is attributable to the body size.
const largeRequestHealthExemptBytes = 2 << 20

// slowLargeRequestThreshold is the elapsed upstream time after which a 401 on a
// large request is treated as request-scoped.
const slowLargeRequestThreshold = 30 * time.Second

// RequestTooLargeError is returned before any upstream call when the body exceeds the guard.
type RequestTooLargeError struct {
	Size  int64
	Limit int64
}

func (e *RequestTooLargeError) Error() string {
	return fmt.Sprintf("请求过大（%s > %s），xAI 上游无法处理，请在 Codex 中执行 /compact 或新开会话 / Request too large (%s > %s): xAI upstream cannot process it, run /compact in Codex or start a new session",
		formatMB(e.Size), formatMB(e.Limit), formatMB(e.Size), formatMB(e.Limit))
}

func formatMB(value int64) string {
	mb := float64(value) / float64(1<<20)
	if mb == float64(int64(mb)) {
		return fmt.Sprintf("%d MB", int64(mb))
	}
	return fmt.Sprintf("%.1f MB", mb)
}

func (s *Service) checkUpstreamRequestSize(size int) *RequestTooLargeError {
	limit := s.maxUpstreamRequestBytes.Load()
	if limit <= 0 || int64(size) <= limit {
		return nil
	}
	return &RequestTooLargeError{Size: int64(size), Limit: limit}
}

func isSlowLargeRequestFailure(largeRequest bool, startedAt time.Time) bool {
	return largeRequest && time.Since(startedAt) >= slowLargeRequestThreshold
}

// newLargeRequestUpstreamFailure reports a slow 401 / Cloudflare 5xx on a large
// body as request_too_large (413) so clients see an actionable message instead
// of the generic credential/availability error.
func newLargeRequestUpstreamFailure(size int, accountID uint64, accountName string, upstreamStatus int) *UpstreamFailure {
	return &UpstreamFailure{
		HTTPStatus: 413, Code: RequestTooLargeCode, AccountID: accountID, AccountName: accountName,
		UpstreamCode: fmt.Sprintf("upstream_%d", upstreamStatus),
		PublicMessage: fmt.Sprintf("请求过大（%s），xAI 上游处理超时（上游返回 %d），请在 Codex 中执行 /compact 或新开会话 / Request too large (%s): xAI upstream timed out (status %d); run /compact or start a new session",
			formatMB(int64(size)), upstreamStatus, formatMB(int64(size)), upstreamStatus),
	}
}

// Request-scoped 401 defaults: a 401 that only arrives after a long upstream
// wait, or on a large body, is usually an upstream/edge artifact of that one
// request rather than proof the account credential is bad.
const (
	DefaultRequestScoped401MinBytes   int64 = 512 << 10
	DefaultRequestScoped401MinElapsed       = 60 * time.Second
)

// RequestScoped401Config tunes when a 401 is attributed to the request.
type RequestScoped401Config struct {
	// MinBytes: bodies at or above this size never mark the account for
	// reauthorization or switch accounts on 401. 0 disables the size rule.
	MinBytes int64
	// MinElapsed: a 401 that arrives after at least this long is request-scoped
	// immediately (no refresh, no reauth mark, no account switch). 0 disables.
	MinElapsed time.Duration
}

// UpdateRequestScoped401 applies the request-scoped 401 heuristics.
func (s *Service) UpdateRequestScoped401(value RequestScoped401Config) {
	if value.MinBytes < 0 {
		value.MinBytes = 0
	}
	if value.MinElapsed < 0 {
		value.MinElapsed = 0
	}
	s.requestScoped401.Store(&value)
}

func (s *Service) requestScoped401Config() RequestScoped401Config {
	if value := s.requestScoped401.Load(); value != nil {
		return *value
	}
	return RequestScoped401Config{MinBytes: DefaultRequestScoped401MinBytes, MinElapsed: DefaultRequestScoped401MinElapsed}
}

// largeFor401 reports whether the body is large enough that a 401 must never
// penalize the account or fan out to other accounts.
func (s *Service) largeFor401(size int) bool {
	cfg := s.requestScoped401Config()
	return cfg.MinBytes > 0 && int64(size) >= cfg.MinBytes
}

// immediateRequestScoped401 reports whether a 401 should be returned at once
// without refresh, reauth or failover: it arrived after a long upstream wait,
// or the body is large and the 401 was not instant.
func (s *Service) immediateRequestScoped401(size int, startedAt time.Time) bool {
	cfg := s.requestScoped401Config()
	elapsed := time.Since(startedAt)
	if cfg.MinElapsed > 0 && elapsed >= cfg.MinElapsed {
		return true
	}
	return (s.largeFor401(size) || size >= largeRequestHealthExemptBytes) && elapsed >= slowLargeRequestThreshold
}

// newRequestScoped401Failure keeps the existing 413 contract for large bodies
// and reports small slow/request-specific 401s as a retryable gateway timeout.
func (s *Service) newRequestScoped401Failure(size int, accountID uint64, accountName string, verified bool) *UpstreamFailure {
	if s.largeFor401(size) || size >= largeRequestHealthExemptBytes {
		return newLargeRequestUpstreamFailure(size, accountID, accountName, http.StatusUnauthorized)
	}
	message := "上游长时间处理后返回 401，这通常是该请求本身的问题而不是账号失效，请重试 / Upstream returned 401 after a long wait; this is request-scoped, please retry"
	if verified {
		message = "上游拒绝了这个请求（401），但账号验证正常，请重试 / Upstream rejected this request with 401 while the account verified fine; please retry"
	}
	return &UpstreamFailure{
		HTTPStatus: http.StatusGatewayTimeout, Code: "upstream_request_scoped_unauthorized", AccountID: accountID, AccountName: accountName,
		UpstreamCode: "upstream_401", PublicMessage: message,
	}
}
