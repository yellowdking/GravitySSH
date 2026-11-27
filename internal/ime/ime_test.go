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

func TestClearTargetsToEnglishTSFFallbackAndFocusGuard(t *testing.T) {
	const (
		focusHWND uintptr = 10
		hostHWND  uintptr = 20
		focusIME  uintptr = 100
		hostIME   uintptr = 200
	)
	defIME := func(h uintptr) uintptr {
		switch h {
		case focusHWND:
			return focusIME
		case hostHWND:
			return hostIME
		default:
			return 0
		}
	}

	// 1. WebView2 TSF on single-layout Chinese Windows: both focus and host report IMM32 mode 0.
	// Must return cleared=false so VK_IME_OFF fallback runs!
	cleared := clearTargetsToEnglish(focusIME, []uintptr{focusHWND, hostHWND}, true, defIME, func(ime, w, l uintptr) (uintptr, bool) {
		return 0, true
	}, nil)
	if cleared {
		t.Fatal("expected cleared=false when focus and host report idle IMM32 mode 0 (TSF WebView2)")
	}

	// 2. Host/sibling HWND transitions 0x0401 -> 0 while focus reports 0: must NOT suppress TSF fallback for focus.
	hostMode := uintptr(0x0401)
	cleared = clearTargetsToEnglish(focusIME, []uintptr{focusHWND, hostHWND}, true, defIME, func(ime, w, l uintptr) (uintptr, bool) {
		if ime == hostIME {
			if w == 1 {
				return hostMode, true
			}
			if w == 2 {
				hostMode = l
				return 1, true
			}
		}
		return 0, true
	}, nil)
	if cleared {
		t.Fatal("expected sibling/host IME transition to NOT mark focus as cleared")
	}

	// 3. Focus's own IME window transitions from native Chinese (0x0401) -> 0: cleared=true.
	focusMode := uintptr(0x0401)
	cleared = clearTargetsToEnglish(focusIME, []uintptr{focusHWND, hostHWND}, true, defIME, func(ime, w, l uintptr) (uintptr, bool) {
		if ime == focusIME {
			if w == 1 {
				return focusMode, true
			}
			if w == 2 {
				focusMode = l
				return 1, true
			}
		}
		return 0, true
	}, nil)
	if !cleared {
		t.Fatal("expected cleared=true when focus's own IME transitioned from native 0x0401 to 0")
	}

	// 4. Focus's own IME window ignores IMC_SETCONVERSIONMODE (stays 0x0401): cleared=false so fallback runs.
	cleared = clearTargetsToEnglish(focusIME, []uintptr{focusHWND}, true, defIME, func(ime, w, l uintptr) (uintptr, bool) {
		if w == 1 {
			return 0x0401, true
		}
		return 1, true
	}, nil)
	if cleared {
		t.Fatal("expected cleared=false when focus's IME remained in native mode after set")
	}
}
