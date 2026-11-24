//go:build !windows

package ime

// SwitchToEnglish is a no-op on non-Windows platforms.
func SwitchToEnglish() {}
