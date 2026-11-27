package main

import (
	"reflect"
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

func TestSplitShortcutExecLinesPreservesHeredocBlankLines(t *testing.T) {
	script := strings.Join([]string{
		"",
		"cat <<'EOF' > /tmp/app.conf",
		"key1=val1",
		"",
		"key2=val2",
		"EOF",
		"",
		"cat <<-END",
		"\tlineA",
		"",
		"\tEND",
		"grep foo <<< \"bar\"",
		"",
		"echo done",
	}, "\n")

	got := splitShortcutExecLines(script)
	want := []string{
		"cat <<'EOF' > /tmp/app.conf",
		"key1=val1",
		"",
		"key2=val2",
		"EOF",
		"cat <<-END",
		"\tlineA",
		"",
		"\tEND",
		"grep foo <<< \"bar\"",
		"echo done",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitShortcutExecLines mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestSessionShortcutRunnerCancelsPriorRunWithoutInterleaving(t *testing.T) {
	var (
		mu      sync.Mutex
		written []string
	)
	writeFn := func(s string) error {
		mu.Lock()
		written = append(written, s)
		mu.Unlock()
		return nil
	}

	var runner sessionShortcutRunner
	// Start run 1 (5 lines, paced at 35ms each).
	if err := runner.run(nil, nil, "r1-1\nr1-2\nr1-3\nr1-4\nr1-5", true, writeFn); err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}
	// Immediately start run 2 on the same session: must cancel run 1 cleanly before writing r2-1.
	if err := runner.run(nil, nil, "r2-1\nr2-2", true, writeFn); err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}
	// Wait for run 2 to finish.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := len(written) >= 3 && written[len(written)-1] == "r2-2\r"
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Extra wait to ensure no leaked r1 lines arrive after r2 finishes.
	time.Sleep(100 * time.Millisecond)
	runner.cancelAndWait()

	mu.Lock()
	defer mu.Unlock()
	r2Start := -1
	for i, line := range written {
		if line == "r2-1\r" {
			r2Start = i
			break
		}
	}
	if r2Start < 0 {
		t.Fatalf("expected r2-1 to be written, got %#v", written)
	}
	// After r2-1 starts, NO r1-* line may appear.
	for _, line := range written[r2Start:] {
		if strings.HasPrefix(line, "r1-") {
			t.Fatalf("run 1 interleaved after run 2 started: %#v", written)
		}
	}
	if written[len(written)-1] != "r2-2\r" {
		t.Fatalf("expected r2-2 as final line, got %#v", written)
	}
}
