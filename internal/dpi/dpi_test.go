//go:build windows

package dpi

import "testing"

func TestEnablePerMonitorV2(t *testing.T) {
	ctx := dpiAwarenessContextPerMonitorAwareV2
	if got := int(ctx); got != -4 {
		t.Fatalf("DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = %d, want -4", got)
	}
	// Must succeed cleanly on Windows 10 1703+ / Windows 11 and be idempotent.
	if !EnablePerMonitorV2() {
		t.Fatalf("expected EnablePerMonitorV2 to succeed on Windows")
	}
	if !EnablePerMonitorV2() {
		t.Fatalf("expected repeated EnablePerMonitorV2 call to remain true")
	}
	if !IsPerMonitorV2() {
		t.Fatalf("expected thread DPI awareness context to be Per-Monitor V2")
	}
}
