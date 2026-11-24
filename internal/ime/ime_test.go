package ime

import "testing"

func TestSwitchToEnglishSafe(t *testing.T) {
	// Should return cleanly without panicking or blocking even when headless / not foreground.
	SwitchToEnglish()
}
