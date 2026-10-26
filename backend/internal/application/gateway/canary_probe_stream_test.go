package gateway

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// blockingBody returns prefix then blocks until ctx is cancelled.
type blockingBody struct {
	prefix *strings.Reader
	ctx    context.Context
}

func (b *blockingBody) Read(p []byte) (int, error) {
	if b.prefix.Len() > 0 {
		return b.prefix.Read(p)
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func TestCanaryStreamIdleTimeoutWithoutFirstToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watchdog := newCanaryWatchdog(50*time.Millisecond, cancel)
	defer watchdog.stop()
	body := &blockingBody{prefix: strings.NewReader(": keepalive\n\ndata: {\"id\":\"r1\",\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"), ctx: ctx}
	usage := Usage{}
	id := ""
	start := time.Now()
	out, err := readCanaryStream(context.Background(), body, start, watchdog, &usage, &id, nil)
	if !errors.Is(err, ErrCanaryTransport) || out.ErrorCode != CanaryErrIdleTimeout {
		t.Fatalf("code=%q err=%v", out.ErrorCode, err)
	}
	if out.StreamBytes == 0 || out.StreamChunks != 1 || out.FirstTokenMS != 0 {
		t.Fatalf("out = %#v", out)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("idle timeout did not fire early")
	}
}

type errAfterReader struct{ r io.Reader }

func (e *errAfterReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err == io.EOF {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

func TestCanaryStreamInterruptedKeepsPartial(t *testing.T) {
	body := &errAfterReader{r: strings.NewReader("data: {\"id\":\"r1\",\"choices\":[{\"delta\":{\"reasoning_content\":\"think\",\"content\":\"Final\"}}]}\n\n")}
	usage := Usage{}
	id := ""
	watchdog := newCanaryWatchdog(time.Minute, func() {})
	defer watchdog.stop()
	out, err := readCanaryStream(context.Background(), body, time.Now(), watchdog, &usage, &id, nil)
	if err == nil || out.ErrorCode != CanaryErrInterrupted {
		t.Fatalf("code=%q err=%v", out.ErrorCode, err)
	}
	if out.Text != "Final" || out.ReasoningText != "think" || out.StreamChunks != 1 {
		t.Fatalf("out = %#v", out)
	}
}

func TestCanaryStreamCompletes(t *testing.T) {
	body := strings.NewReader("data: {\"id\":\"r1\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"id\":\"r1\",\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5},\"choices\":[]}\n\ndata: [DONE]\n\n")
	usage := Usage{}
	id := ""
	watchdog := newCanaryWatchdog(time.Minute, func() {})
	defer watchdog.stop()
	out, err := readCanaryStream(context.Background(), body, time.Now(), watchdog, &usage, &id, nil)
	if err != nil || out.ErrorCode != "" || out.Text != "ok" || out.OutputTokens != 2 || id != "r1" || out.StreamChunks != 2 {
		t.Fatalf("out=%#v err=%v", out, err)
	}
}

func TestCanaryStreamVisibleCap(t *testing.T) {
	chunk := "data: {\"id\":\"r1\",\"choices\":[{\"delta\":{\"content\":\"1 2 3 4 5 \"}}]}\n\n"
	body := &errAfterReader{r: strings.NewReader(strings.Repeat(chunk, 50))}
	usage := Usage{}
	id := ""
	watchdog := newCanaryWatchdog(time.Minute, func() {})
	defer watchdog.stop()
	out, err := readCanaryStreamLimited(context.Background(), body, time.Now(), watchdog, &usage, &id, nil, 25)
	if err != nil || out.ErrorCode != CanaryErrVisibleCapped || len(out.Text) != 30 {
		t.Fatalf("code=%q err=%v len=%d", out.ErrorCode, err, len(out.Text))
	}
}
