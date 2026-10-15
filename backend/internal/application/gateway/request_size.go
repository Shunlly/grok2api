package gateway

import (
	"fmt"
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
