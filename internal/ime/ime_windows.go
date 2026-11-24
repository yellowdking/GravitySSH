//go:build windows

package ime

import (
	"sync"
	"syscall"
	"unsafe"
)

const (
	wmImeControl        = 0x0283
	imcGetConversion    = 0x0001
	imcSetConversion    = 0x0002
	imcGetOpenStatus    = 0x0005
	imcSetOpenStatus    = 0x0006
	imeCmodeNative      = 0x0001
	imeCmodeKatakana    = 0x0002
	imeCmodeFullShape   = 0x0008
	smtoAbortIfHung     = 0x0002
	imeSendTimeoutMs    = 80
)

type rect struct {
	Left, Top, Right, Bottom int32
}

type guiThreadInfo struct {
	CbSize        uint32
	Flags         uint32
	HwndActive    uintptr
	HwndFocus     uintptr
	HwndCapture   uintptr
	HwndMenuOwner uintptr
	HwndMoveSize  uintptr
	HwndCaret     uintptr
	RcCaret       rect
}

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	imm32                   = syscall.NewLazyDLL("imm32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
	procGetGUIThreadInfo    = user32.NewProc("GetGUIThreadInfo")
	procEnumChildWindows    = user32.NewProc("EnumChildWindows")
	procSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")
	procImmGetDefaultIMEWnd = imm32.NewProc("ImmGetDefaultIMEWnd")
	procImmGetContext       = imm32.NewProc("ImmGetContext")
	procImmSetConvStatus    = imm32.NewProc("ImmSetConversionStatus")
	procImmSetOpenStatus    = imm32.NewProc("ImmSetOpenStatus")
	procImmReleaseContext   = imm32.NewProc("ImmReleaseContext")
	procGetCurrentProcessId = kernel32.NewProc("GetCurrentProcessId")
)

var (
	enumMu       sync.Mutex
	enumChildren []uintptr
	enumCB       = syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		enumChildren = append(enumChildren, hwnd)
		return 1
	})
)

// SwitchToEnglish switches the active Windows IME to half-width English mode
// when GravitySSH is the foreground window (including embedded WebView2 child HWNDs).
func SwitchToEnglish() {
	fg, _, _ := procGetForegroundWindow.Call()
	if fg == 0 {
		return
	}

	var fgPID uint32
	fgTID, _, _ := procGetWindowThreadPID.Call(fg, uintptr(unsafe.Pointer(&fgPID)))
	curPID, _, _ := procGetCurrentProcessId.Call()
	if fgPID == 0 || uintptr(fgPID) != curPID {
		return
	}

	seen := make(map[uintptr]struct{}, 16)
	targets := make([]uintptr, 0, 16)
	addTarget := func(h uintptr) {
		if h == 0 {
			return
		}
		if _, ok := seen[h]; ok {
			return
		}
		seen[h] = struct{}{}
		targets = append(targets, h)
	}

	if focus := focusedWindowOfThread(fgTID); focus != 0 {
		addTarget(focus)
	}

	children := collectChildWindows(fg)
	seenThreads := map[uintptr]bool{fgTID: true}
	for _, ch := range children {
		var chPID uint32
		chTID, _, _ := procGetWindowThreadPID.Call(ch, uintptr(unsafe.Pointer(&chPID)))
		if chTID != 0 && !seenThreads[chTID] {
			seenThreads[chTID] = true
			if focus := focusedWindowOfThread(chTID); focus != 0 {
				addTarget(focus)
			}
		}
		addTarget(ch)
	}
	addTarget(fg)

	for _, hwnd := range targets {
		switchWindowIMEToEnglish(hwnd)
	}
}

func focusedWindowOfThread(tid uintptr) uintptr {
	if tid == 0 {
		return 0
	}
	var info guiThreadInfo
	info.CbSize = uint32(unsafe.Sizeof(info))
	r, _, _ := procGetGUIThreadInfo.Call(tid, uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0
	}
	return info.HwndFocus
}

func collectChildWindows(parent uintptr) []uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumChildren = enumChildren[:0]
	_, _, _ = procEnumChildWindows.Call(parent, enumCB, 0)
	out := make([]uintptr, len(enumChildren))
	copy(out, enumChildren)
	return out
}

func switchWindowIMEToEnglish(hwnd uintptr) {
	if imeWnd, _, _ := procImmGetDefaultIMEWnd.Call(hwnd); imeWnd != 0 {
		openStatus, okOpen := sendIMEControl(imeWnd, imcGetOpenStatus, 0)
		convMode, okConv := sendIMEControl(imeWnd, imcGetConversion, 0)
		needsSwitch := !okOpen || openStatus != 0 || !okConv || (convMode&(imeCmodeNative|imeCmodeKatakana|imeCmodeFullShape)) != 0
		if needsSwitch {
			_, _ = sendIMEControl(imeWnd, imcSetConversion, 0)
			_, _ = sendIMEControl(imeWnd, imcSetOpenStatus, 0)
		}
	}

	if himc, _, _ := procImmGetContext.Call(hwnd); himc != 0 {
		_, _, _ = procImmSetConvStatus.Call(himc, 0, 0)
		_, _, _ = procImmSetOpenStatus.Call(himc, 0)
		_, _, _ = procImmReleaseContext.Call(hwnd, himc)
	}
}

func sendIMEControl(imeWnd, wParam, lParam uintptr) (uintptr, bool) {
	var result uintptr
	r, _, _ := procSendMessageTimeoutW.Call(
		imeWnd,
		wmImeControl,
		wParam,
		lParam,
		smtoAbortIfHung,
		imeSendTimeoutMs,
		uintptr(unsafe.Pointer(&result)),
	)
	return result, r != 0
}
