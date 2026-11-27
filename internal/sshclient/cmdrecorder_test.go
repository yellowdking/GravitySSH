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

	// Up arrow followed by typing extra args must keep dirtyHist until Enter/Ctrl+C
	// so partial appended fragments are NOT logged as standalone commands.
	rec.Feed([]byte("\x1b[A --verbose"))
	if got := rec.CurrentLine(); got != "" {
		t.Fatalf("expected CurrentLine empty while dirtyHist active, got %q", got)
	}
	rec.Feed([]byte("\r"))
	if len(recorded) != 0 {
		t.Fatalf("expected no recorded fragment after Up+typing+Enter, got %#v", recorded)
	}

	// Up arrow + typing + Ctrl+C resets cleanly so next fresh command records normally.
	rec.Feed([]byte("\x1b[B --extra\x03"))
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

func TestCommandRecorderSuppressesLeadingSpaceSecretsAndAltScreen(t *testing.T) {
	var recorded []string
	rec := NewCommandRecorder(func(cmd string) {
		recorded = append(recorded, cmd)
	})

	// 1. HISTCONTROL=ignorespace: leading space or tab is ignored before TrimSpace.
	rec.Feed([]byte(" export API_TOKEN=secret"))
	if got := rec.CurrentLine(); got != "" {
		t.Fatalf("expected CurrentLine empty for leading-space command, got %q", got)
	}
	rec.Feed([]byte("\r"))
	rec.Feed([]byte("\tmysql -u root -pSecret\n"))
	// Trailing spaces without leading space are still recorded normally.
	rec.Feed([]byte("ls -la   \r"))

	// 2. Interactive sudo password prompt (including pwfeedback stars).
	rec.FeedOutput([]byte("alice@host:~$ sudo systemctl restart nginx\r\n[sudo] password for alice: "))
	rec.Feed([]byte("MyS3cr3t"))
	if got := rec.CurrentLine(); got != "" {
		t.Fatalf("expected CurrentLine suppressed at password prompt, got %q", got)
	}
	rec.FeedOutput([]byte("********")) // sudo Defaults pwfeedback
	rec.Feed([]byte("\r"))
	rec.FeedOutput([]byte("\r\nalice@host:~$ "))
	rec.Feed([]byte("whoami\r"))

	// 3. ANSI-colored passphrase prompt, split chunks, Chinese password prompt, and OTP prompt.
	rec.FeedOutput([]byte("\x1b[1;33mEnter passphrase for key '/root/.ssh/id_ed25519':\x1b[0m "))
	rec.Feed([]byte("key-passphrase-123\r"))

	rec.FeedOutput([]byte("\r\n[sudo] alice 的"))
	rec.FeedOutput([]byte("密码："))
	rec.Feed([]byte("chinese-pw\r"))

	rec.FeedOutput([]byte("\r\nVerification code: "))
	rec.Feed([]byte("123456\r"))

	rec.FeedOutput([]byte("\r\nEnter OTP: "))
	rec.Feed([]byte("654321\r"))

	// 4. Normal command after prompts resumes recording cleanly.
	rec.FeedOutput([]byte("\r\nalice@host:~$ "))
	rec.Feed([]byte("uptime\r"))

	// 5. Alternate screen (vim / less / htop) suppresses input & does not leak keys on exit.
	rec.FeedOutput([]byte("\x1b[?104"))
	rec.FeedOutput([]byte("9h"))
	rec.Feed([]byte(":set paste\r"))
	rec.Feed([]byte("q")) // e.g. quitting less/htop without Enter
	if got := rec.CurrentLine(); got != "" {
		t.Fatalf("expected CurrentLine empty in alt-screen, got %q", got)
	}
	rec.FeedOutput([]byte("\x1b[?1049;2004l"))
	rec.Feed([]byte("df -h\r"))

	// Legacy ?47h / ?47l alt-screen
	rec.FeedOutput([]byte("\x1b[?47h"))
	rec.Feed([]byte("hidden-in-47\r"))
	rec.FeedOutput([]byte("\x1b[?47l"))
	rec.Feed([]byte("pwd\r"))

	want := []string{"ls -la", "whoami", "uptime", "df -h", "pwd"}
	if !reflect.DeepEqual(recorded, want) {
		t.Fatalf("recorded mismatch:\ngot:  %#v\nwant: %#v", recorded, want)
	}
}
