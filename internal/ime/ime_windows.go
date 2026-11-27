//go:build windows

package ime

import (
	"sync"
	"syscall"
	"unsafe"
)

const imeCmodeFullShape = 0x0008

type guiThreadInfo struct {
	CbSize, Flags                                      uint32
	Active, Focus, Capture, MenuOwner, MoveSize, Caret uintptr
	RcCaret                                            [4]int32
}

type keybdInput struct {
	wVk, wScan    uint16
	dwFlags, time uint32
	extraInfo     uintptr
	pad           [2]uint32
}

type winInput struct {
	typ uint32
	ki  keybdInput
}

var (
	u32          = syscall.NewLazyDLL("user32.dll")
	i32          = syscall.NewLazyDLL("imm32.dll")
	pFgWnd       = u32.NewProc("GetForegroundWindow")
	pAncestor    = u32.NewProc("GetAncestor")
	pWndTPID     = u32.NewProc("GetWindowThreadProcessId")
	pGuiInfo     = u32.NewProc("GetGUIThreadInfo")
	pEnumChild   = u32.NewProc("EnumChildWindows")
	pSendMsgTO   = u32.NewProc("SendMessageTimeoutW")
	pPostMsg     = u32.NewProc("PostMessageW")
	pGetKL       = u32.NewProc("GetKeyboardLayout")
	pGetKLList   = u32.NewProc("GetKeyboardLayoutList")
	pKeyState    = u32.NewProc("GetAsyncKeyState")
	pSendInput   = u32.NewProc("SendInput")
	pDefIMEWnd   = i32.NewProc("ImmGetDefaultIMEWnd")
	pImmCtx      = i32.NewProc("ImmGetContext")
	pImmSetConv  = i32.NewProc("ImmSetConversionStatus")
	pImmSetOpen  = i32.NewProc("ImmSetOpenStatus")
	pImmRelCtx   = i32.NewProc("ImmReleaseContext")
	pCurPID  = syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentProcessId")
	swMu     sync.Mutex
	enumKids []uintptr
	enumCB   = syscall.NewCallback(func(h, _ uintptr) uintptr { enumKids = append(enumKids, h); return 1 })
)

// SwitchToEnglish switches Windows IME to half-width English when GravitySSH owns the foreground window.
func SwitchToEnglish() {
	if !swMu.TryLock() {
		return
	}
	defer swMu.Unlock()

	fg, _, _ := pFgWnd.Call()
	cur, _, _ := pCurPID.Call()
	if fg == 0 || cur == 0 || !ownedBy(fg, cur) {
		return
	}
	fgTID, _ := threadPID(fg)
	enumKids = enumKids[:0]
	pEnumChild.Call(fg, enumCB, 0)
	kids := append([]uintptr(nil), enumKids...)

	seen := map[uintptr]bool{}
	targets := make([]uintptr, 0, len(kids)+4)
	add := func(h uintptr) {
		if h != 0 && !seen[h] {
			seen[h] = true
			targets = append(targets, h)
		}
	}

	focus := focusOf(0)
	add(focus)
	seenT := map[uintptr]bool{}
	for _, ch := range append(kids, fg) {
		if tid, _ := threadPID(ch); tid != 0 && !seenT[tid] {
			seenT[tid] = true
			if f := focusOf(tid); f != 0 {
				if focus == 0 {
					focus = f
				}
				add(f)
			}
		}
	}
	for _, ch := range kids {
		add(ch)
	}
	add(fg)
	if focus == 0 {
		focus = fg
	}

	tid, _ := threadPID(focus)
	hkl, _, _ := pGetKL.Call(tid)
	if hkl == 0 && fgTID != 0 {
		hkl, _, _ = pGetKL.Call(fgTID)
	}
	if isEnglishHKL(hkl) {
		return
	}

	// 1. Switch via WM_INPUTLANGCHANGEREQUEST when an English layout is already installed.
	if en := findInstalledEnglishHKL(); en != 0 {
		pPostMsg.Call(focus, 0x0050, 0, en)
		if fg != focus {
			pPostMsg.Call(fg, 0x0050, 0, en)
		}
		return
	}

	// 2. Clear native/full-shape conversion mode without closing Microsoft Pinyin openStatus.
	zh, cleared := isChineseHKL(hkl), false
	seenIME := map[uintptr]bool{}
	for _, h := range targets {
		if ime, _, _ := pDefIMEWnd.Call(h); ime != 0 && !seenIME[ime] {
			seenIME[ime] = true
			if toEnglish(ime, zh) {
				cleared = true
			}
		}
		if himc, _, _ := pImmCtx.Call(h); himc != 0 {
			pImmSetConv.Call(himc, 0, 0)
			if !zh {
				pImmSetOpen.Call(himc, 0)
			}
			pImmRelCtx.Call(h, himc)
		}
	}

	// 3. Idempotent VK_IME_OFF fallback for TSF-only WebView2 windows that ignore IMM32.
	if !cleared && !modifierDown() {
		if f, _, _ := pFgWnd.Call(); f != 0 && ownedBy(f, cur) {
			in := [2]winInput{
				{typ: 1, ki: keybdInput{wVk: 0x1A}},
				{typ: 1, ki: keybdInput{wVk: 0x1A, dwFlags: 0x0002}},
			}
			pSendInput.Call(2, uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
		}
	}
}

func toEnglish(ime uintptr, zh bool) bool {
	open, okOpen := imeCtl(ime, 5, 0) // IMC_GETOPENSTATUS
	before, okB := imeCtl(ime, 1, 0)  // IMC_GETCONVERSIONMODE
	hadNative := okB && needsEnglishConversion(before)
	if zh {
		if okB && !hadNative {
			return true
		}
		imeCtl(ime, 2, 0) // IMC_SETCONVERSIONMODE = 0 (half-width English)
		after, okA := imeCtl(ime, 1, 0)
		if okA && !needsEnglishConversion(after) {
			return true
		}
		if okA && needsEnglishConversion(after) {
			imeCtl(ime, 6, 0) // IMC_SETOPENSTATUS = 0 fallback for legacy IMEs
			imeCtl(ime, 2, 0)
		}
		return false
	}
	if okOpen && open == 0 && okB && !hadNative {
		return true
	}
	if !okOpen || open != 0 || !okB || hadNative {
		imeCtl(ime, 2, 0)
		imeCtl(ime, 6, 0)
		return hadNative || (okOpen && open != 0)
	}
	return false
}

func isEnglishHKL(h uintptr) bool           { return h != 0 && uint16(h&0xFFFF)&0x03FF == 0x09 }
func isChineseHKL(h uintptr) bool           { return h != 0 && uint16(h&0xFFFF)&0x03FF == 0x04 }
func needsEnglishConversion(m uintptr) bool { return m&(0x0001|0x0002|imeCmodeFullShape) != 0 }

func selectEnglishHKL(hkls []uintptr) uintptr {
	var fb uintptr
	for _, h := range hkls {
		if !isEnglishHKL(h) {
			continue
		}
		if uint16(h&0xFFFF) == 0x0409 {
			return h
		}
		if fb == 0 {
			fb = h
		}
	}
	return fb
}

func findInstalledEnglishHKL() uintptr {
	n, _, _ := pGetKLList.Call(0, 0)
	if n == 0 || n > 64 {
		return 0
	}
	list := make([]uintptr, n)
	got, _, _ := pGetKLList.Call(n, uintptr(unsafe.Pointer(&list[0])))
	if got == 0 {
		return 0
	}
	if got > n {
		got = n
	}
	return selectEnglishHKL(list[:got])
}

func threadPID(h uintptr) (uintptr, uintptr) {
	var pid uint32
	tid, _, _ := pWndTPID.Call(h, uintptr(unsafe.Pointer(&pid)))
	return tid, uintptr(pid)
}

func ownedBy(h, cur uintptr) bool {
	if _, pid := threadPID(h); pid != 0 && pid == cur {
		return true
	}
	root, _, _ := pAncestor.Call(h, 3)
	_, pid := threadPID(root)
	return root != 0 && pid == cur
}

func focusOf(tid uintptr) uintptr {
	var gi guiThreadInfo
	gi.CbSize = uint32(unsafe.Sizeof(gi))
	if r, _, _ := pGuiInfo.Call(tid, uintptr(unsafe.Pointer(&gi))); r == 0 {
		return 0
	}
	return gi.Focus
}

func imeCtl(ime, w, l uintptr) (uintptr, bool) {
	var res uintptr
	r, _, _ := pSendMsgTO.Call(ime, 0x0283, w, l, 0x0002, 80, uintptr(unsafe.Pointer(&res)))
	return res, r != 0
}

func modifierDown() bool {
	for _, vk := range [...]uintptr{0x10, 0x11, 0x12, 0x5B, 0x5C} {
		if s, _, _ := pKeyState.Call(vk); s&0x8000 != 0 {
			return true
		}
	}
	return false
}
