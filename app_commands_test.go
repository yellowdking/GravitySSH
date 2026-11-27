package main

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// Compile-time verification that exported Wails signature stays intact.
var _ func(*App, string, string, bool) error = (*App).RunShortcut

func TestFormatShortcutFillInputSafety(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "single_line_no_newline",
			raw:  "ls -la",
			want: "ls -la",
		},
		{
			name: "single_line_trailing_crlf_stripped",
			raw:  "rm -rf /tmp/test\r\n\r\n",
			want: "rm -rf /tmp/test",
		},
		{
			name: "multiline_wrapped_in_bracketed_paste",
			raw:  "echo one\r\necho two\n",
			want: "\x1b[200~echo one\necho two\x1b[201~",
		},
		{
			name: "multiline_nested_breakout_stripped_iteratively",
			raw:  "echo safe\n\x1b[20\x1b[201~1~\necho after\x9b201~",
			want: "\x1b[200~echo safe\n\necho after\x1b[201~",
		},
		{
			name: "only_newlines_becomes_empty",
			raw:  "\r\n\n\r",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatShortcutFillInput(tc.raw)
			if got != tc.want {
				t.Fatalf("formatShortcutFillInput(%q) = %q, want %q", tc.raw, got, tc.want)
			}
			if strings.HasSuffix(got, "\r") || strings.HasSuffix(got, "\n") {
				t.Fatalf("fill input must never end with CR/LF: %q", got)
			}
			if strings.HasPrefix(got, "\x1b[200~") && strings.HasSuffix(got, "\x1b[201~") {
				inner := got[len("\x1b[200~") : len(got)-len("\x1b[201~")]
				for _, forbidden := range []string{"\x1b[200~", "\x1b[201~", "\x9b200~", "\x9b201~"} {
					if strings.Contains(inner, forbidden) {
						t.Fatalf("bracketed-paste inner payload still contains %q: %q", forbidden, inner)
					}
				}
			}
		})
	}
}

func TestRunShortcutPayloadFillVsExecute(t *testing.T) {
	// execute=false MUST NEVER append \r even for multi-line scripts.
	var writes []string
	err := runShortcutPayload(nil, nil, "echo a\necho b\n", false, func(s string) error {
		writes = append(writes, s)
		return nil
	})
	if err != nil {
		t.Fatalf("fill runShortcutPayload failed: %v", err)
	}
	if len(writes) != 1 || writes[0] != "\x1b[200~echo a\necho b\x1b[201~" {
		t.Fatalf("unexpected fill writes: %#v", writes)
	}

	// execute=true sends first line immediately with \r and subsequent lines paced.
	var mu sync.Mutex
	var execWrites []string
	done := make(chan struct{})
	err = runShortcutPayload(nil, nil, "echo first\n\necho second\n", true, func(s string) error {
		mu.Lock()
		execWrites = append(execWrites, s)
		n := len(execWrites)
		mu.Unlock()
		if n == 2 {
			close(done)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("execute runShortcutPayload failed: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for second executed line; got %#v", execWrites)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(execWrites) != 2 || execWrites[0] != "echo first\r" || execWrites[1] != "echo second\r" {
		t.Fatalf("unexpected execute writes: %#v", execWrites)
	}
}
