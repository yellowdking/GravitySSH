//go:build unix

package main

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestStartReloadSignalReceivesSIGHUP(t *testing.T) {
	ch, stop := startReloadSignal()
	defer stop()
	if ch == nil {
		t.Fatal("expected non-nil reload signal channel on unix")
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP to self: %v", err)
	}

	select {
	case sig := <-ch:
		if sig != syscall.SIGHUP {
			t.Fatalf("expected SIGHUP, got %v", sig)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SIGHUP on reload signal channel")
	}
}
