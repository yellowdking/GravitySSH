package sshclient

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	maxRecorderBufRunes    = 8 * 1024
	maxRecorderEscLen      = 64
	maxRecorderOutEscLen   = 64
	maxRecorderOutStrLen   = 4096
	maxRecorderLastLineLen = 512
)

type outParseState uint8

const (
	outStateNormal outParseState = iota
	outStateEsc
	outStateCSI
	outStateOSC
	outStateOSCEsc
	outStateStr
	outStateStrEsc
)

// CommandRecorder tracks keystrokes typed into an interactive PTY terminal
// and reconstructs executed commands cleanly for history recording & autocomplete,
// suppressing commands with leading whitespace, alternate-screen TUI sessions,
// and interactive password/passphrase/OTP prompts.
type CommandRecorder struct {
	mu             sync.Mutex
	buf            []rune
	cursor         int
	inEsc          bool
	escSeq         strings.Builder
	dirtyHist      bool // set when user uses Up/Down shell history so we don't log malformed partials
	inAltScreen    bool // set while terminal is in alternate screen buffer (vim, nano, htop, less, ...)
	awaitingSecret bool // set when recent terminal output ended with a password/passphrase/OTP prompt
	outState       outParseState
	outEscBuf      []byte
	outStrLen      int
	lastLine       []byte
	onCommand      func(cmd string)
}

func NewCommandRecorder(onCommand func(cmd string)) *CommandRecorder {
	return &CommandRecorder{onCommand: onCommand}
}

func hasLeadingSpace(s string) bool {
	if s == "" {
		return false
	}
	rn, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(rn)
}

// CurrentLine returns the current uncommitted command text in the input line.
func (r *CommandRecorder) CurrentLine() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dirtyHist || r.inAltScreen || r.awaitingSecret {
		return ""
	}
	line := string(r.buf)
	if hasLeadingSpace(line) {
		return ""
	}
	return line
}

// FeedOutput observes raw PTY stdout/stderr bytes from the remote server to detect
// alternate-screen transitions (\x1b[?1049h/l, \x1b[?1047h/l, \x1b[?47h/l) and
// interactive password/passphrase/OTP prompts.
func (r *CommandRecorder) FeedOutput(data []byte) {
	if len(data) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, b := range data {
		switch r.outState {
		case outStateNormal:
			switch b {
			case 0x1b:
				r.outState = outStateEsc
			case '\r', '\n':
				r.lastLine = r.lastLine[:0]
				r.awaitingSecret = false
			case 0x08: // BS
				if len(r.lastLine) > 0 {
					_, sz := utf8.DecodeLastRune(r.lastLine)
					if sz <= 0 {
						sz = 1
					}
					r.lastLine = r.lastLine[:len(r.lastLine)-sz]
				}
			default:
				if b >= 0x20 && b != 0x7f && len(r.lastLine) < maxRecorderLastLineLen {
					r.lastLine = append(r.lastLine, b)
				}
			}

		case outStateEsc:
			switch b {
			case '[':
				r.outState = outStateCSI
				r.outEscBuf = r.outEscBuf[:0]
			case ']':
				r.outState = outStateOSC
				r.outStrLen = 0
			case 'P', 'X', '^', '_':
				r.outState = outStateStr
				r.outStrLen = 0
			default:
				r.outState = outStateNormal
			}

		case outStateCSI:
			if b >= 0x40 && b <= 0x7e {
				r.handleOutputCSILocked(string(r.outEscBuf), b)
				r.outEscBuf = r.outEscBuf[:0]
				r.outState = outStateNormal
			} else if len(r.outEscBuf) >= maxRecorderOutEscLen {
				r.outEscBuf = r.outEscBuf[:0]
				r.outState = outStateNormal
			} else {
				r.outEscBuf = append(r.outEscBuf, b)
			}

		case outStateOSC:
			if b == 0x07 {
				r.outState = outStateNormal
			} else if b == 0x1b {
				r.outState = outStateOSCEsc
			} else {
				r.outStrLen++
				if r.outStrLen >= maxRecorderOutStrLen {
					r.outState = outStateNormal
				}
			}

		case outStateOSCEsc:
			if b == '\\' {
				r.outState = outStateNormal
			} else if b == 0x1b {
				// stay in outStateOSCEsc
			} else {
				r.outState = outStateOSC
			}

		case outStateStr:
			if b == 0x1b {
				r.outState = outStateStrEsc
			} else {
				r.outStrLen++
				if r.outStrLen >= maxRecorderOutStrLen {
					r.outState = outStateNormal
				}
			}

		case outStateStrEsc:
			if b == '\\' {
				r.outState = outStateNormal
			} else if b == 0x1b {
				// stay in outStateStrEsc
			} else {
				r.outState = outStateStr
			}
		}
	}

	if !r.inAltScreen && !r.awaitingSecret && len(r.lastLine) > 0 {
		if looksLikeSecretPrompt(string(r.lastLine)) {
			r.awaitingSecret = true
		}
	}
}

func (r *CommandRecorder) handleOutputCSILocked(params string, final byte) {
	if (final == 'h' || final == 'l') && strings.HasPrefix(params, "?") {
		entering := final == 'h'
		for _, p := range strings.Split(params[1:], ";") {
			if p == "47" || p == "1047" || p == "1049" {
				if entering != r.inAltScreen {
					r.inAltScreen = entering
					r.buf = nil
					r.cursor = 0
					r.dirtyHist = false
					if entering {
						r.awaitingSecret = false
						r.lastLine = r.lastLine[:0]
					}
				}
			}
		}
		return
	}
	if (final == 'K' && params == "2") || (final == 'J' && (params == "2" || params == "3")) {
		r.lastLine = r.lastLine[:0]
		r.awaitingSecret = false
	}
}

func isASCIIWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func containsASCIIWord(s, word string) bool {
	start := 0
	for {
		idx := strings.Index(s[start:], word)
		if idx < 0 {
			return false
		}
		pos := start + idx
		leftOK := pos == 0 || !isASCIIWordChar(s[pos-1])
		end := pos + len(word)
		rightOK := end == len(s) || !isASCIIWordChar(s[end])
		if leftOK && rightOK {
			return true
		}
		start = pos + 1
	}
}

func looksLikeSecretPrompt(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" || len(s) > 200 {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(s)
	if last != ':' && last != '：' && last != '?' && last != '？' {
		return false
	}
	for _, kw := range []string{"密码", "口令", "验证码", "通行短语"} {
		if strings.Contains(s, kw) {
			return true
		}
	}
	low := strings.ToLower(s)
	for _, kw := range []string{
		"password", "passwd", "passphrase", "passcode",
		"verification code", "authenticator code", "security code",
		"one-time", "2fa", "mfa", "totp", "hotp",
		"secret key", "access token", "api key", "private key",
	} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return containsASCIIWord(low, "pin") || containsASCIIWord(low, "otp")
}

// Feed processes raw bytes sent from xterm.js to the SSH PTY stdin.
func (r *CommandRecorder) Feed(data []byte) {
	if len(data) == 0 {
		return
	}

	// Strip bracketed paste wrappers if present (\x1b[200~ ... \x1b[201~)
	s := string(data)
	s = strings.ReplaceAll(s, "\x1b[200~", "")
	s = strings.ReplaceAll(s, "\x1b[201~", "")

	var emitted []string

	r.mu.Lock()
	for len(s) > 0 {
		rn, size := utf8.DecodeRuneInString(s)
		s = s[size:]

		if r.inEsc {
			r.escSeq.WriteRune(rn)
			seq := r.escSeq.String()
			if isEscapeSequenceDone(seq) {
				r.handleEscape(seq)
				r.inEsc = false
				r.escSeq.Reset()
			} else if r.escSeq.Len() >= maxRecorderEscLen {
				r.inEsc = false
				r.escSeq.Reset()
			}
			continue
		}

		switch rn {
		case 0x1b: // ESC
			r.inEsc = true
			r.escSeq.Reset()

		case '\r', '\n':
			if !r.dirtyHist && !r.inAltScreen && !r.awaitingSecret {
				rawLine := string(r.buf)
				if !hasLeadingSpace(rawLine) {
					cmd := strings.TrimSpace(rawLine)
					if cmd != "" {
						emitted = append(emitted, cmd)
					}
				}
			}
			r.buf = nil
			r.cursor = 0
			r.dirtyHist = false
			r.awaitingSecret = false

		case 0x7f, 0x08: // Backspace / DEL
			if !r.inAltScreen && r.cursor > 0 && len(r.buf) > 0 {
				r.buf = append(r.buf[:r.cursor-1], r.buf[r.cursor:]...)
				r.cursor--
			}

		case 0x03, 0x04: // Ctrl+C / Ctrl+D
			r.buf = nil
			r.cursor = 0
			r.dirtyHist = false
			r.awaitingSecret = false

		case 0x15: // Ctrl+U (delete to start of line)
			if r.cursor > 0 {
				r.buf = r.buf[r.cursor:]
				r.cursor = 0
			}
			r.dirtyHist = false

		case 0x0b: // Ctrl+K (delete to end of line)
			if r.cursor < len(r.buf) {
				r.buf = r.buf[:r.cursor]
			}

		case 0x17: // Ctrl+W (delete previous word)
			r.deletePrevWord()

		case 0x01: // Ctrl+A (Home)
			r.cursor = 0

		case 0x05: // Ctrl+E (End)
			r.cursor = len(r.buf)

		case '\t':
			// Preserve leading tab when part of a pasted line (HISTCONTROL=ignorespace),
			// while ignoring standalone interactive Tab completion keystrokes.
			if !r.inAltScreen && len(r.buf) == 0 && len(s) > 0 {
				r.insertRune('\t')
			}
			continue

		default:
			if rn >= 0x20 {
				r.insertRune(rn)
			}
		}
	}
	r.mu.Unlock()

	if r.onCommand != nil {
		for _, cmd := range emitted {
			r.onCommand(cmd)
		}
	}
}

func (r *CommandRecorder) deletePrevWord() {
	if r.cursor <= 0 {
		return
	}
	i := r.cursor
	for i > 0 && r.buf[i-1] == ' ' {
		i--
	}
	for i > 0 && r.buf[i-1] != ' ' {
		i--
	}
	r.buf = append(r.buf[:i], r.buf[r.cursor:]...)
	r.cursor = i
}

func (r *CommandRecorder) insertRune(rn rune) {
	if r.inAltScreen {
		return
	}
	if r.dirtyHist {
		r.buf = nil
		r.cursor = 0
		r.dirtyHist = false
	}
	if len(r.buf) >= maxRecorderBufRunes {
		return
	}
	if r.cursor > len(r.buf) {
		r.cursor = len(r.buf)
	}
	r.buf = append(r.buf, 0)
	copy(r.buf[r.cursor+1:], r.buf[r.cursor:])
	r.buf[r.cursor] = rn
	r.cursor++
}

func isEscapeSequenceDone(seq string) bool {
	if len(seq) == 0 {
		return false
	}
	first := seq[0]
	if first == '[' || first == 'O' {
		if len(seq) == 1 {
			return false
		}
		last := seq[len(seq)-1]
		return (last >= 'A' && last <= 'Z') || (last >= 'a' && last <= 'z') || last == '~'
	}
	return true
}

func (r *CommandRecorder) handleEscape(seq string) {
	switch seq {
	case "[D", "OD": // Left arrow
		if r.cursor > 0 {
			r.cursor--
		}
	case "[C", "OC": // Right arrow
		if r.cursor < len(r.buf) {
			r.cursor++
		}
	case "[H", "OH", "[1~": // Home
		r.cursor = 0
	case "[F", "OF", "[4~": // End
		r.cursor = len(r.buf)
	case "[3~": // Delete forward
		if r.cursor < len(r.buf) {
			r.buf = append(r.buf[:r.cursor], r.buf[r.cursor+1:]...)
		}
	case "[A", "OA", "[B", "OB": // Up / Down (shell history navigation)
		r.buf = nil
		r.cursor = 0
		r.dirtyHist = true
	}
}
