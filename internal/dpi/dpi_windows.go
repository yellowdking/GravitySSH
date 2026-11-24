//go:build windows

package dpi

import (
	"sync"
	"syscall"
)

// Windows DPI awareness context handle values (windef.h).
const (
	dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(3) // (DPI_AWARENESS_CONTEXT)-4
	processPerMonitorDPIAware            = 2           // PROCESS_PER_MONITOR_DPI_AWARE
	errAccessDenied                      = syscall.Errno(5)
)

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	shcore                = syscall.NewLazyDLL("shcore.dll")
	pSetProcessDpiCtx     = user32.NewProc("SetProcessDpiAwarenessContext")
	pGetThreadDpiCtx      = user32.NewProc("GetThreadDpiAwarenessContext")
	pAreDpiCtxEqual       = user32.NewProc("AreDpiAwarenessContextsEqual")
	pSetProcessDPIAware   = user32.NewProc("SetProcessDPIAware")
	pSetProcessDpiAwareEx = shcore.NewProc("SetProcessDpiAwareness")

	enableOnce   sync.Once
	enableResult bool
)

// EnablePerMonitorV2 opts the current process into Windows Per-Monitor V2 DPI
// awareness as early as possible (belt-and-suspenders alongside the PE manifest
// so `go run`, dev builds, and any manifest edge case still render crisp HiDPI).
func EnablePerMonitorV2() bool {
	enableOnce.Do(func() {
		enableResult = applyPerMonitorV2()
	})
	return enableResult
}

func applyPerMonitorV2() bool {
	if pSetProcessDpiCtx.Find() == nil {
		r, _, err := pSetProcessDpiCtx.Call(dpiAwarenessContextPerMonitorAwareV2)
		if r != 0 {
			return true
		}
		// ERROR_ACCESS_DENIED means awareness was already configured via PE manifest.
		if err == errAccessDenied {
			return IsPerMonitorV2()
		}
	}
	if pSetProcessDpiAwareEx.Find() == nil {
		hr, _, _ := pSetProcessDpiAwareEx.Call(processPerMonitorDPIAware)
		// S_OK (0) or E_ACCESSDENIED (0x80070005, already set)
		if hr == 0 || uint32(hr) == 0x80070005 {
			return true
		}
	}
	if pSetProcessDPIAware.Find() == nil {
		r, _, _ := pSetProcessDPIAware.Call()
		return r != 0
	}
	return false
}

// IsPerMonitorV2 reports whether the current thread runs in Per-Monitor V2 DPI mode.
func IsPerMonitorV2() bool {
	if pGetThreadDpiCtx.Find() != nil || pAreDpiCtxEqual.Find() != nil {
		return false
	}
	cur, _, _ := pGetThreadDpiCtx.Call()
	if cur == 0 {
		return false
	}
	eq, _, _ := pAreDpiCtxEqual.Call(cur, dpiAwarenessContextPerMonitorAwareV2)
	return eq != 0
}
