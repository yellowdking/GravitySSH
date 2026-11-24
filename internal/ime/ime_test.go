//go:build windows

package ime

import (
	"testing"
	"unsafe"
)

func TestSwitchToEnglishSafe(t *testing.T) {
	// Should return cleanly without panicking or blocking even when headless / not foreground.
	SwitchToEnglish()
}

func TestHKLClassificationAndSelection(t *testing.T) {
	const (
		zhCN = uintptr(0x08040804)
		zhTW = uintptr(0x04040404)
		jaJP = uintptr(0x04110411)
		enUS = uintptr(0x04090409)
		enGB = uintptr(0x08090809)
	)

	if !isChineseHKL(zhCN) || !isChineseHKL(zhTW) {
		t.Fatalf("expected zh-CN and zh-TW to be detected as Chinese HKLs")
	}
	if isChineseHKL(enUS) || isChineseHKL(jaJP) {
		t.Fatalf("unexpected Chinese HKL detection for non-Chinese layout")
	}
	if !isEnglishHKL(enUS) || !isEnglishHKL(enGB) || isEnglishHKL(zhCN) {
		t.Fatalf("unexpected English HKL classification")
	}
	if got := selectEnglishHKL([]uintptr{zhCN, enGB, enUS}); got != enUS {
		t.Fatalf("expected EN-US preferred, got 0x%x", got)
	}
	if got := selectEnglishHKL([]uintptr{zhCN, enGB}); got != enGB {
		t.Fatalf("expected EN-GB fallback, got 0x%x", got)
	}
	if got := selectEnglishHKL([]uintptr{zhCN, jaJP}); got != 0 {
		t.Fatalf("expected 0 when no English HKL installed, got 0x%x", got)
	}
}

func TestNeedsEnglishConversion(t *testing.T) {
	if needsEnglishConversion(0) {
		t.Fatalf("expected mode 0 (half-width English) to not require switch")
	}
	if !needsEnglishConversion(0x0401) {
		t.Fatalf("expected Microsoft Pinyin Chinese mode (0x0401) to require switch")
	}
	if !needsEnglishConversion(imeCmodeFullShape) {
		t.Fatalf("expected full-shape mode to require switch")
	}
}

func TestWinInputStructLayout(t *testing.T) {
	var wantSize uintptr = 28
	if unsafe.Sizeof(uintptr(0)) == 8 {
		wantSize = 40
	}
	if got := unsafe.Sizeof(winInput{}); got != wantSize {
		t.Fatalf("winInput sizeof = %d, want %d", got, wantSize)
	}
}
