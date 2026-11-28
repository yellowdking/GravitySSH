package sshclient

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

type noopWriteCloser struct {
	mu      sync.Mutex
	written int
	onChunk func(total int)
}

func (w *noopWriteCloser) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.written += len(p)
	total := w.written
	cb := w.onChunk
	w.mu.Unlock()
	if cb != nil {
		cb(total)
	}
	return len(p), nil
}

func (w *noopWriteCloser) Close() error { return nil }

func TestSessionExecContextCancelsImmediatelyOnStopCh(t *testing.T) {
	mgr := NewManager(nil, nil)
	stopCh := make(chan struct{})
	mgr.mu.Lock()
	mgr.sessions["sess-exec"] = &sessionState{stopCh: stopCh}
	mgr.mu.Unlock()

	ctx, cancel := mgr.sessionExecContext("sess-exec", 30*time.Second)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("ctx should not be cancelled before stopCh closes")
	default:
	}

	close(stopCh)
	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected sessionExecContext to cancel immediately when stopCh closes")
	}
	// Double cancel must be safe (idempotent sync.Once).
	cancel()
	cancel()
}

func TestWriteInputAbortsLargePasteImmediatelyOnStopCh(t *testing.T) {
	mgr := NewManager(nil, nil)
	stopCh := make(chan struct{})
	var closeOnce sync.Once
	sink := &noopWriteCloser{
		onChunk: func(total int) {
			if total >= writeChunkSize {
				closeOnce.Do(func() { close(stopCh) })
			}
		},
	}
	mgr.mu.Lock()
	mgr.sessions["sess-paste"] = &sessionState{
		stdin:  sink,
		stopCh: stopCh,
	}
	mgr.mu.Unlock()

	// 256 KiB payload would take ~1.25s if sleeping 5ms across all 256 chunks.
	bigPaste := strings.Repeat("A", 256*writeChunkSize)
	start := time.Now()
	err := mgr.WriteInput("sess-paste", bigPaste)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected WriteInput to return error when stopCh closes mid-paste")
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("expected immediate paste abort on stopCh close, took %v", elapsed)
	}
	if bytes.Repeat([]byte("A"), sink.written) == nil || sink.written >= len(bigPaste) {
		t.Fatalf("expected partial write before abort, wrote %d / %d", sink.written, len(bigPaste))
	}
}
