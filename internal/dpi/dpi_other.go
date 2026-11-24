//go:build !windows

package dpi

// EnablePerMonitorV2 is a no-op on non-Windows platforms.
func EnablePerMonitorV2() bool { return false }

// IsPerMonitorV2 is a no-op on non-Windows platforms.
func IsPerMonitorV2() bool { return false }
