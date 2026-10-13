package inference

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// SSE keepalive for streaming /v1/responses and /v1/chat/completions.
//
// Large Codex contexts routinely wait 30-60s+ for the first upstream byte, and
// some upstream phases are silent for long stretches. Proxies (new-api,
// Cloudflare, nginx) and clients with idle timeouts may cut such connections.
// The keepalive writer:
//   - while the gateway is still waiting for upstream response headers, commits
//     a 200 text/event-stream response after `grace` and emits ": keepalive"
//     comments every `interval`. Requests that fail before `grace` keep their
//     normal HTTP error status. After an early commit, any JSON error body the
//     handler writes is converted to an in-stream error event.
//   - during the stream, injects a comment only at SSE event boundaries (the
//     last forwarded chunk ended with a blank line) after `interval` of silence,
//     so event ordering and framing are never disturbed.
//   - stops when the handler returns.
var (
	sseKeepaliveInterval = 15 * time.Second
	sseKeepaliveGrace    = 15 * time.Second
	sseKeepaliveTick     = time.Second
)

var sseKeepaliveComment = []byte(": keepalive\n\n")

type sseKeepaliveWriter struct {
	gin.ResponseWriter
	protocol streamProtocol

	mu             sync.Mutex
	started        time.Time
	lastWrite      time.Time
	boundary       bool
	sse            bool
	committedEarly bool
	settled        bool
	comments       int

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// startSSEKeepalive wraps c.Writer; callers must defer the returned stop.
func startSSEKeepalive(c *gin.Context, protocol streamProtocol) (*sseKeepaliveWriter, func()) {
	w := &sseKeepaliveWriter{ResponseWriter: c.Writer, protocol: protocol, started: time.Now(), boundary: true, stop: make(chan struct{}), done: make(chan struct{})}
	c.Writer = w
	go w.loop(sseKeepaliveInterval, sseKeepaliveGrace, sseKeepaliveTick)
	return w, func() {
		w.once.Do(func() { close(w.stop); <-w.done })
	}
}

func (w *sseKeepaliveWriter) loop(interval, grace, tick time.Duration) {
	defer close(w.done)
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-w.stop:
			return
		case now := <-ticker.C:
			w.mu.Lock()
			switch {
			case !w.ResponseWriter.Written():
				if !w.settled && now.Sub(w.started) >= grace {
					header := w.ResponseWriter.Header()
					header.Set("Content-Type", "text/event-stream")
					header.Set("Cache-Control", "no-cache")
					header.Set("X-Accel-Buffering", "no")
					w.ResponseWriter.WriteHeader(http.StatusOK)
					w.committedEarly, w.sse = true, true
					w.writeCommentLocked(now)
				}
			case w.sse && w.boundary && now.Sub(w.lastWrite) >= interval:
				w.writeCommentLocked(now)
			}
			w.mu.Unlock()
		}
	}
}

func (w *sseKeepaliveWriter) writeCommentLocked(now time.Time) {
	_ = setResponseWriteDeadline(w.ResponseWriter)
	if _, err := w.ResponseWriter.Write(sseKeepaliveComment); err == nil {
		w.ResponseWriter.Flush()
		w.comments++
	}
	w.lastWrite = now
	w.boundary = true
}

// settle marks that the gateway call returned; from now on the handler owns
// headers and the keepalive never commits a response on its own.
func (w *sseKeepaliveWriter) settle() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.settled = true
	return w.committedEarly
}

func (w *sseKeepaliveWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	payload := data
	if w.committedEarly && !looksLikeSSE(data) {
		payload = convertJSONErrorToSSE(w.protocol, data)
	}
	if !w.ResponseWriter.Written() {
		w.sse = looksLikeSSE(payload)
	}
	if _, err := w.ResponseWriter.Write(payload); err != nil {
		return 0, err
	}
	if len(payload) > 0 {
		w.lastWrite = time.Now()
		w.boundary = bytes.HasSuffix(payload, []byte("\n\n"))
	}
	return len(data), nil
}

func (w *sseKeepaliveWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

func (w *sseKeepaliveWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ResponseWriter.Flush()
}

func (w *sseKeepaliveWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *sseKeepaliveWriter) keepaliveCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.comments
}

func looksLikeSSE(data []byte) bool {
	trimmed := bytes.TrimLeft(data, "\r\n")
	return bytes.HasPrefix(trimmed, []byte("event:")) || bytes.HasPrefix(trimmed, []byte("data:")) ||
		bytes.HasPrefix(trimmed, []byte(":")) || bytes.HasPrefix(trimmed, []byte("id:")) || bytes.HasPrefix(trimmed, []byte("retry:"))
}

// convertJSONErrorToSSE turns an OpenAI-style JSON error body written after an
// early SSE commit into a terminal in-stream error event.
func convertJSONErrorToSSE(protocol streamProtocol, data []byte) []byte {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(bytes.TrimSpace(data), &envelope)
	code := "upstream_error"
	if value, ok := envelope.Error.Code.(string); ok && value != "" {
		code = value
	}
	message := envelope.Error.Message
	if message == "" {
		message = "上游服务暂不可用"
	}
	if protocol == streamProtocolChat {
		payload, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]any{"code": code, "message": message, "type": "server_error"}})
		return []byte("data: " + string(payload) + "\n\ndata: [DONE]\n\n")
	}
	event := map[string]any{
		"type": "response.failed",
		"response": map[string]any{
			"object": "response", "status": "failed", "output": []any{},
			"error": map[string]any{"code": code, "message": message},
		},
	}
	payload, _ := json.Marshal(event)
	return []byte("event: response.failed\ndata: " + string(payload) + "\n\n")
}
