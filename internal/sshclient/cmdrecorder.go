package sshclient

import (
	"strings"
	"sync"
	"unicode/utf8"
)

type CommandRecorder struct {
	mu        sync.Mutex
	buf       []rune
	cursor    int
	inEsc     bool
	escSeq    strings.Builder
	dirtyHist bool
	onCommand func(cmd string)
}

func NewCommandRecorder(onCommand func(cmd string)) *CommandRecorder {
	return &CommandRecorder{onCommand: onCommand}
}

func (r *CommandRecorder) CurrentLine() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dirtyHist {
		return ""
	}
	return string(r.buf)
}

func (r *CommandRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = nil
	r.cursor = 0
	r.inEsc = false
	r.escSeq.Reset()
	r.dirtyHist = false
}

func (r *CommandRecorder) Feed(data []byte) {
	if len(data) == 0 {
		return
	}

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
			}
			continue
		}

		switch rn {
		case 0x1b:
			r.inEsc = true
			r.escSeq.Reset()

		case '\r', '\n':
			if !r.dirtyHist {
				cmd := strings.TrimSpace(string(r.buf))
				if cmd != "" && !isLikelyNoise(cmd) {
					emitted = append(emitted, cmd)
				}
			}
			r.buf = nil
			r.cursor = 0
			r.dirtyHist = false

		case 0x7f, 0x08:
			if r.cursor > 0 && len(r.buf) > 0 {
				r.buf = append(r.buf[:r.cursor-1], r.buf[r.cursor:]...)
				r.cursor--
			}

		case 0x03, 0x04:
			r.buf = nil
			r.cursor = 0
			r.dirtyHist = false

		case 0x15:
			if r.cursor > 0 {
				r.buf = r.buf[r.cursor:]
				r.cursor = 0
			}
			r.dirtyHist = false

		case 0x0b:
			if r.cursor < len(r.buf) {
				r.buf = r.buf[:r.cursor]
			}

		case 0x17:
			if r.cursor > 0 {
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

		case 0x01:
			r.cursor = 0

		case 0x05:
			r.cursor = len(r.buf)

		case '\t':
			continue

		default:
			if rn >= 0x20 {
				if r.dirtyHist {
					r.buf = nil
					r.cursor = 0
					r.dirtyHist = false
				}
				r.buf = append(r.buf, 0)
				copy(r.buf[r.cursor+1:], r.buf[r.cursor:])
				r.buf[r.cursor] = rn
				r.cursor++
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
	case "[D", "OD":
		if r.cursor > 0 {
			r.cursor--
		}
	case "[C", "OC":
		if r.cursor < len(r.buf) {
			r.cursor++
		}
	case "[H", "OH", "[1~":
		r.cursor = 0
	case "[F", "OF", "[4~":
		r.cursor = len(r.buf)
	case "[3~":
		if r.cursor < len(r.buf) {
			r.buf = append(r.buf[:r.cursor], r.buf[r.cursor+1:]...)
		}
	case "[A", "OA", "[B", "OB":
		r.buf = nil
		r.cursor = 0
		r.dirtyHist = true
	}
}

func isLikelyNoise(cmd string) bool {
	return strings.TrimSpace(cmd) == ""
}
