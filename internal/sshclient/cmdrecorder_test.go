package sshclient

import (
	"reflect"
	"testing"
)

func TestCommandRecorderBasicAndEditing(t *testing.T) {
	var recorded []string
	rec := NewCommandRecorder(func(cmd string) {
		recorded = append(recorded, cmd)
	})

	// Simple command
	rec.Feed([]byte("docker ps -a\r"))
	// Backspace correction
	rec.Feed([]byte("apt updaet\x7f\x7fte\r"))
	// Ctrl+C cancels line
	rec.Feed([]byte("rm -rf /tmp/oops\x03"))
	// Ctrl+U clears line then new command
	rec.Feed([]byte("wrong\x15systemctl status nginx\n"))
	// Left arrow edit
	rec.Feed([]byte("ls -l\x1b[Da\r"))
	// Bracketed paste with multiple lines
	rec.Feed([]byte("\x1b[200~apk update\napk add curl\n\x1b[201~"))
	// Ctrl+W deletes previous word
	rec.Feed([]byte("git checkout wrong\x17main\r"))

	want := []string{
		"docker ps -a",
		"apt update",
		"systemctl status nginx",
		"ls -al",
		"apk update",
		"apk add curl",
		"git checkout main",
	}
	if !reflect.DeepEqual(recorded, want) {
		t.Fatalf("recorded mismatch:\ngot:  %#v\nwant: %#v", recorded, want)
	}
}

func TestCommandRecorderCurrentLineAndUpArrow(t *testing.T) {
	var recorded []string
	rec := NewCommandRecorder(func(cmd string) {
		recorded = append(recorded, cmd)
	})

	rec.Feed([]byte("dock"))
	if got := rec.CurrentLine(); got != "dock" {
		t.Fatalf("expected CurrentLine 'dock', got %q", got)
	}

	// Up arrow enters shell history navigation -> should not emit garbled escape sequence
	rec.Feed([]byte("\x1b[A\r"))
	if len(recorded) != 0 {
		t.Fatalf("expected no recorded command on up-arrow history recall, got %#v", recorded)
	}

	// Typing again resets cleanly
	rec.Feed([]byte("free -h\r"))
	if len(recorded) != 1 || recorded[0] != "free -h" {
		t.Fatalf("expected ['free -h'], got %#v", recorded)
	}
}

func TestCommandRecorderCapsBufferAt8KB(t *testing.T) {
	var emitted string
	rec := NewCommandRecorder(func(cmd string) { emitted = cmd })
	huge := make([]byte, maxRecorderBufRunes+2048)
	for i := range huge {
		huge[i] = 'a'
	}
	rec.Feed(huge)
	if got := len(rec.CurrentLine()); got != maxRecorderBufRunes {
		t.Fatalf("expected CurrentLine capped at %d, got %d", maxRecorderBufRunes, got)
	}
	rec.Feed([]byte("\r"))
	if len(emitted) != maxRecorderBufRunes {
		t.Fatalf("expected emitted command capped at %d, got %d", maxRecorderBufRunes, len(emitted))
	}
}
