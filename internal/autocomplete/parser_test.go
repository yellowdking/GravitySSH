package autocomplete

import (
	"reflect"
	"testing"

	"github.com/yellowdking/GravitySSH/internal/models"
)

func TestSinglePassParseLineEdgeCasesAndSuggestReuse(t *testing.T) {
	cases := []struct {
		name          string
		input         string
		wantSegPrefix string
		wantActiveSeg string
		wantToks      []string
		wantEndSp     bool
		wantQuote     byte
	}{
		{"empty", "", "", "", nil, false, 0},
		{"spaces_only", "   \t ", "   \t ", "", nil, false, 0},
		{"simple_trailing_space", "ls -la ", "", "ls -la ", []string{"ls", "-la"}, true, 0},
		{"semicolon_split", "echo a; cat /etc/hosts", "echo a; ", "cat /etc/hosts", []string{"cat", "/etc/hosts"}, false, 0},
		{"semicolon_trailing_ws", "echo a;   ", "echo a;   ", "", nil, false, 0},
		{"and_or_pipe_amp", "a && b || c |& grep foo", "a && b || c |& ", "grep foo", []string{"grep", "foo"}, false, 0},
		{"redirection_amp_not_split", "cmd 2>&1 &>out ", "", "cmd 2>&1 &>out ", []string{"cmd", "2>&1", "&>out"}, true, 0},
		{"quoted_separator_not_split", `echo "a;b|c" 'd&&e'`, "", `echo "a;b|c" 'd&&e'`, []string{"echo", "a;b|c", "d&&e"}, false, 0},
		{"escaped_separator_not_split", `cat foo\;bar`, "", `cat foo\;bar`, []string{"cat", "foo;bar"}, false, 0},
		{"unclosed_double_quote", `cd "my dir`, "", `cd "my dir`, []string{"cd", "my dir"}, false, '"'},
		{"unclosed_single_quote", `cd 'my dir`, "", `cd 'my dir`, []string{"cd", "my dir"}, false, '\''},
		{"double_quote_escaped_quote", `echo "a\"b" `, "", `echo "a\"b" `, []string{"echo", `a"b`}, true, 0},
		{"double_quote_non_special_backslash", `echo "a\nb"`, "", `echo "a\nb"`, []string{"echo", `a\nb`}, false, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := ParseLine(tc.input, "/home/u", "/home/u")
			if p.SegPrefix != tc.wantSegPrefix || p.ActiveSeg != tc.wantActiveSeg || p.EndsWithSpace != tc.wantEndSp {
				t.Fatalf("ParseLine(%q): segPrefix=%q active=%q endSp=%v, want (%q, %q, %v)",
					tc.input, p.SegPrefix, p.ActiveSeg, p.EndsWithSpace, tc.wantSegPrefix, tc.wantActiveSeg, tc.wantEndSp)
			}
			var gotToks []string
			for _, tk := range p.Tokens {
				gotToks = append(gotToks, tk.Unquoted)
			}
			if !reflect.DeepEqual(gotToks, tc.wantToks) {
				t.Fatalf("ParseLine(%q) tokens = %#v, want %#v", tc.input, gotToks, tc.wantToks)
			}
			if p.CurTok.Quote != tc.wantQuote {
				t.Fatalf("ParseLine(%q) curQuote = %q, want %q", tc.input, p.CurTok.Quote, tc.wantQuote)
			}
		})
	}

	// Verify SuggestWithContext reuses pre-parsed ParsedLine identically.
	entries := []models.SFTPFileEntry{
		{Name: "app.conf", Path: "/etc/app.conf", IsDir: false, Size: 128},
		{Name: "nginx", Path: "/etc/nginx", IsDir: true},
	}
	parsed := ParseLine("cat /etc/a", "/home/u", "/home/u")
	withParsed := SuggestWithContext(SuggestRequest{
		Input: "cat /etc/a", CWD: "/home/u", HomeDir: "/home/u",
		Parsed: &parsed, TargetEntries: entries, Limit: 5,
	})
	withoutParsed := SuggestWithContext(SuggestRequest{
		Input: "cat /etc/a", CWD: "/home/u", HomeDir: "/home/u",
		TargetEntries: entries, Limit: 5,
	})
	if !reflect.DeepEqual(withParsed, withoutParsed) || len(withParsed) != 1 || withParsed[0].InsertText != "cat /etc/app.conf" {
		t.Fatalf("Parsed reuse mismatch: with=%+v without=%+v", withParsed, withoutParsed)
	}
}