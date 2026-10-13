package inference

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type lockedRecorder struct {
	*httptest.ResponseRecorder
	mu sync.Mutex
}

func (r *lockedRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Write(b)
}

func (r *lockedRecorder) body() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Body.String()
}

func withFastKeepalive(t *testing.T) {
	oldI, oldG, oldT := sseKeepaliveInterval, sseKeepaliveGrace, sseKeepaliveTick
	sseKeepaliveInterval, sseKeepaliveGrace, sseKeepaliveTick = 40*time.Millisecond, 40*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { sseKeepaliveInterval, sseKeepaliveGrace, sseKeepaliveTick = oldI, oldG, oldT })
}

func newKeepaliveContext() (*gin.Context, *lockedRecorder) {
	gin.SetMode(gin.TestMode)
	rec := &lockedRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, rec
}

func TestSSEKeepaliveCommitsEarlyAndConvertsLateError(t *testing.T) {
	withFastKeepalive(t)
	c, rec := newKeepaliveContext()
	w, stop := startSSEKeepalive(c, streamProtocolResponses)
	time.Sleep(120 * time.Millisecond)
	if !w.settle() {
		t.Fatal("expected early commit while waiting for upstream")
	}
	writeOpenAIError(c, http.StatusBadGateway, "upstream_cloudflare_error", "boom")
	stop()
	body := rec.body()
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.HasPrefix(body, ": keepalive\n\n") {
		t.Fatalf("keepalive must precede everything, got %q", body)
	}
	if !strings.Contains(body, "event: response.failed\ndata: ") || !strings.Contains(body, "upstream_cloudflare_error") || strings.Contains(body, "\n{\"error\"") {
		t.Fatalf("late JSON error must become response.failed: %q", body)
	}
}

func TestSSEKeepaliveFastErrorKeepsHTTPStatus(t *testing.T) {
	withFastKeepalive(t)
	c, rec := newKeepaliveContext()
	w, stop := startSSEKeepalive(c, streamProtocolResponses)
	if w.settle() {
		t.Fatal("must not commit before grace")
	}
	writeOpenAIError(c, http.StatusTooManyRequests, "rate", "slow down")
	time.Sleep(100 * time.Millisecond)
	stop()
	if rec.Code != http.StatusTooManyRequests || strings.Contains(rec.body(), "keepalive") {
		t.Fatalf("fast errors must keep their status and JSON body: %d %q", rec.Code, rec.body())
	}
}

func TestSSEKeepaliveOnlyAtEventBoundaries(t *testing.T) {
	withFastKeepalive(t)
	c, rec := newKeepaliveContext()
	w, stop := startSSEKeepalive(c, streamProtocolResponses)
	w.settle()
	c.Header("Content-Type", "text/event-stream")
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write([]byte("event: response.created\ndata: {}\n\n"))
	time.Sleep(100 * time.Millisecond) // silence at a boundary -> comment
	_, _ = c.Writer.Write([]byte("event: response.output_text.delta\ndata: {\"delta\":\"a"))
	time.Sleep(100 * time.Millisecond) // silence mid-event -> no comment
	_, _ = c.Writer.Write([]byte("\"}\n\n"))
	stop()
	countAfterStop := w.keepaliveCount()
	time.Sleep(60 * time.Millisecond)
	body := rec.body()
	if !strings.Contains(body, "data: {}\n\n: keepalive\n\n") {
		t.Fatalf("expected keepalive after boundary: %q", body)
	}
	if strings.Contains(body, "\"delta\":\"a: keepalive") || !strings.Contains(body, "{\"delta\":\"a\"}\n\n") {
		t.Fatalf("keepalive corrupted an event: %q", body)
	}
	if w.keepaliveCount() != countAfterStop {
		t.Fatal("keepalive must stop when the handler returns")
	}
}

func TestSSEKeepaliveChatLateError(t *testing.T) {
	out := string(convertJSONErrorToSSE(streamProtocolChat, []byte(`{"error":{"code":"x","message":"m"}}`)))
	if !strings.HasPrefix(out, "data: ") || !strings.HasSuffix(out, "data: [DONE]\n\n") {
		t.Fatalf("chat error conversion: %q", out)
	}
}
