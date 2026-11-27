package autocomplete

import (
	"strings"
	"testing"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

func sq(in string) []models.CompletionItem {
	return SuggestWithContext(SuggestRequest{Input: in, Limit: 5})
}

func TestZeroShortcutsAndMidWordNoise(t *testing.T) {
	for _, it := range SuggestWithContext(SuggestRequest{Input: "docker ", Limit: 8}) {
		if it.Source == "shortcut" {
			t.Fatalf("shortcuts forbidden: %+v", it)
		}
	}
	for _, q := range []string{"f", "ls", "do"} {
		for _, it := range sq(q) {
			if !strings.HasPrefix(strings.ToLower(it.Command), q) {
				t.Fatalf("noise %q: %+v", q, it)
			}
		}
	}
}

func TestOneCharTriggerAndFreeFlow(t *testing.T) {
	if f := sq("f"); len(f) < 3 || f[0].Command != "free" || f[0].InsertText != "free " || f[1].Command != "find" || f[2].Command != "firewall-cmd" {
		t.Fatalf("f: %+v", f)
	}
	if fs := sq("free "); len(fs) == 0 || fs[0].Command != "-h" || fs[0].InsertText != "free -h" {
		t.Fatalf("free : %+v", fs)
	}
	if ff := sq("free -"); len(ff) == 0 || ff[0].Source != "flag" || ff[0].Command != "-h" {
		t.Fatalf("free -: %+v", ff)
	}
}

func TestDockerSubcmdsChainingAndWrappers(t *testing.T) {
	if d := sq("docker "); len(d) < 2 || d[0].Command != "ps -a" || d[1].Command != "compose" || d[1].InsertText != "docker compose " {
		t.Fatalf("d: %+v", d)
	}
	if dc := sq("docker c"); len(dc) == 0 || dc[0].Command != "compose" || dc[0].InsertText != "docker compose " || dc[0].Source != "subcmd" {
		t.Fatalf("dc: %+v", dc)
	}
	if dcp := sq("docker compose "); len(dcp) < 3 || dcp[0].Command != "up -d" || dcp[1].Command != "down" || dcp[2].Command != "pull" {
		t.Fatalf("dcp: %+v", dcp)
	}
	if ch := sq("cd /opt && docker c"); len(ch) == 0 || ch[0].Command != "compose" || ch[0].InsertText != "cd /opt && docker compose " {
		t.Fatalf("ch: %+v", ch)
	}
	if su := sq("sudo docker c"); len(su) == 0 || su[0].Command != "compose" || su[0].InsertText != "sudo docker compose " {
		t.Fatalf("su: %+v", su)
	}
}

func TestContextualFlagsAndDedup(t *testing.T) {
	if f := sq("ls -"); len(f) == 0 || f[0].Source != "flag" || f[0].Command != "-la" || f[0].InsertText != "ls -la" {
		t.Fatalf("ls-: %+v", f)
	}
	up := sq("docker compose up -d -")
	if len(up) == 0 || up[0].Command != "--build" || up[0].InsertText != "docker compose up -d --build" {
		t.Fatalf("dedup: %+v", up)
	}
	for _, it := range up {
		if it.Command == "-d" || it.Source != "flag" {
			t.Fatalf("-d: %+v", up)
		}
	}
	// FileFlags value arg skipped when resolving flag context: docker compose -f compose.yml up - -> docker compose up flags
	withFile := sq("docker compose -f compose.yml up -")
	if len(withFile) == 0 || withFile[0].Source != "flag" || withFile[0].Command != "-d" {
		t.Fatalf("file-flag skip failed: %+v", withFile)
	}
}

func TestFilesDirsOnlyExtBoostAndHome(t *testing.T) {
	es := []models.SFTPFileEntry{
		{Name: "app", IsDir: true}, {Name: "my dir", IsDir: true},
		{Name: "nginx.conf", Size: 2048}, {Name: "app.bin", Size: 4096}, {Name: ".env", Size: 128},
	}
	fq := func(in string) []models.CompletionItem {
		return SuggestWithContext(SuggestRequest{Input: in, CWD: "/srv", CWDEntries: es, Limit: 8})
	}
	if cd := fq("cd "); len(cd) != 2 || cd[0].Source != "dir" || cd[1].InsertText != `cd my\ dir/` {
		t.Fatalf("cd: %+v", cd)
	}
	if n := fq("nano "); len(n) != 4 || n[0].Command != "nginx.conf" || n[3].Command != "app.bin" || n[0].Score <= n[1].Score || n[1].Score <= n[3].Score {
		t.Fatalf("nano: %+v", n)
	}
	if ef := fq("docker compose --env-file "); len(ef) == 0 || ef[0].Command != ".env" {
		t.Fatalf("env: %+v", ef)
	}
	if dir, ok := ExtractPathContextWithHome("cd ~/work/a", "/srv", "/home/alice"); !ok || dir != "/home/alice/work" {
		t.Fatalf("~/: %q %v", dir, ok)
	}
	// Empty homeDir must NOT default to "/"
	if dir, ok := ExtractPathContextWithHome("cd ~/work/a", "/srv", ""); ok || dir != "" {
		t.Fatalf("empty homeDir should not expand to /: dir=%q ok=%v", dir, ok)
	}
	// Quoted ~/ must NOT expand to homeDir
	if dir, ok := ExtractPathContextWithHome(`cd "~/work/a`, "/srv", "/home/alice"); !ok || dir != "/srv/~/work" {
		t.Fatalf("quoted ~/ should stay relative to cwd: dir=%q ok=%v", dir, ok)
	}
	if _, ok := ExtractPathContextWithHome("docker c", "/srv", "/home/alice"); ok {
		t.Fatal("suppress")
	}

	// $HOME/... must NOT escape leading $ while literal $ in filename IS escaped
	if got := EscapePathToken("$HOME/", "app", true, 0); got != "$HOME/app/" {
		t.Fatalf("unquoted $HOME escaped: %q", got)
	}
	if got := EscapePathToken("$HOME/my dir/", "price$5.txt", false, 0); got != `$HOME/my\ dir/price\$5.txt` {
		t.Fatalf("mixed $HOME + $file: %q", got)
	}
	if got := EscapePathToken("${HOME}/", "my dir", true, '"'); got != `"${HOME}/my dir/` {
		t.Fatalf("double-quoted ${HOME}: %q", got)
	}
	if dir, ok := ExtractPathContextWithHome("cd $HOME/work/a", "/srv", "/home/alice"); !ok || dir != "/home/alice/work" {
		t.Fatalf("$HOME/: %q %v", dir, ok)
	}
}

func TestFolderContextCwdBoost(t *testing.T) {
	cwd := []models.SFTPFileEntry{{Name: "docker-compose.yml", Size: 300}, {Name: ".git", IsDir: true}}
	q := func(in string) []models.CompletionItem {
		return SuggestWithContext(SuggestRequest{Input: in, CWD: "/opt", CWDEntries: cwd, Limit: 5})
	}
	if d := q("docker "); len(d) == 0 || d[0].Command != "compose" {
		t.Fatalf("d: %+v", d)
	}
	if do := q("do"); len(do) == 0 || do[0].Command != "docker compose up -d" {
		t.Fatalf("do: %+v", do)
	}
	if g := q("gi"); len(g) == 0 || g[0].Command != "git status" {
		t.Fatalf("gi: %+v", g)
	}
}

func TestHistoryAffinityFrecencyAndDenylist(t *testing.T) {
	now := time.Now()
	mk := func(c, sid string, d time.Duration) models.CommandRecord {
		return models.CommandRecord{Command: c, ServerID: sid, ExecutedAt: now.Add(d)}
	}
	hist := []models.CommandRecord{
		mk("curl https://b.io/api", "s2", -time.Hour),
		mk("curl https://a.io/ok", "s1", -2*time.Hour),
		mk("curl https://a.io/ok", "s1", -10*time.Minute),
		mk("rm -rf /", "s1", 0),
		mk("sudo rm -rf /*", "s1", 0),
		mk("/bin/rm -rf /", "s1", 0),
		mk("cat zero | dd of=/dev/sda", "s1", 0),
		mk("rm -rf / & echo ok", "s1", 0),
		mk("TOKEN=abc curl https://s.io", "s1", 0),
		mk("AWS_SECRET_ACCESS_KEY=xyz aws s3 ls", "s1", 0),
		mk("PASSWD=123 mysql", "s1", 0),
		mk("API_KEY=sk-123 curl https://s.io", "s1", 0),
		mk(`curl -H "Authorization: Bearer secret123" https://s.io`, "s1", 0),
		mk("mkfs.ext4 /dev/sda1", "s1", 0),
		mk("reboot", "s1", 0),
	}
	hq := func(in string) []models.CompletionItem {
		return SuggestWithContext(SuggestRequest{Input: in, ServerID: "s1", History: hist, IncludeHistory: true, Limit: 5})
	}
	if it := hq("curl https://"); len(it) != 2 || it[0].Source != "history_local" || it[0].Category != "本机历史" || it[1].Source != "history" {
		t.Fatalf("hist: %+v", it)
	}
	for _, q := range []string{"rm -r", "sudo rm", "/bin/rm", "cat zero", "mkfs", "rebo", "TOKEN=", "AWS_SECRET", "PASSWD=", "API_KEY=", "curl -H"} {
		for _, it := range hq(q) {
			if it.Source == "history_local" || it.Source == "history" {
				t.Fatalf("deny %q: %+v", q, it)
			}
		}
	}
	if r := hq("cd /opt && curl https://a"); len(r) == 0 || r[0].InsertText != "cd /opt && curl https://a.io/ok" {
		t.Fatalf("rebase: %+v", r)
	}
}

func TestAutocompleteP2Hardening(t *testing.T) {
	now := time.Now()
	cwdEntries := []models.SFTPFileEntry{
		{Name: "Makefile", Size: 100},
		{Name: "makefile", Size: 200},
		{Name: "README.md", Size: 300},
		{Name: "app.conf", Size: 400},
	}

	// 1. Preserve quoted multi-space strings in history InsertText (no destructive normWS).
	hist := []models.CommandRecord{
		{Command: `grep "foo  bar" app.log`, ServerID: "s1", ExecutedAt: now},
		{Command: `/usr/bin/sudo rm -rf /`, ServerID: "s1", ExecutedAt: now},
		{Command: `sudo -u=root rm -rf /`, ServerID: "s1", ExecutedAt: now},
		{Command: `rm -rf //`, ServerID: "s1", ExecutedAt: now},
		{Command: `rm -rf ~/*`, ServerID: "s1", ExecutedAt: now},
		{Command: `curl -u admin:s3cr3t https://example.com`, ServerID: "s1", ExecutedAt: now},
		{Command: `curl https://admin:s3cr3t@example.com`, ServerID: "s1", ExecutedAt: now},
		{Command: `sshpass -p hunter2 ssh root@10.0.0.1`, ServerID: "s1", ExecutedAt: now},
		{Command: `curl -H "Authorization: Basic dXNlcjpwYXNz" https://example.com`, ServerID: "s1", ExecutedAt: now},
	}
	quotedRes := SuggestWithContext(SuggestRequest{
		Input: `grep "foo  `, ServerID: "s1", History: hist, IncludeHistory: true, Limit: 5,
	})
	if len(quotedRes) != 1 || quotedRes[0].InsertText != `grep "foo  bar" app.log` {
		t.Fatalf("expected quoted double-space preserved in InsertText, got %+v", quotedRes)
	}

	// 2. Tightened denylist and secretPattern bypasses must all be suppressed.
	for _, q := range []string{"/usr/bin/sudo", "sudo -u=root", "rm -rf /", "rm -rf ~", "curl -u", "curl https://admin", "sshpass", "curl -H"} {
		for _, it := range SuggestWithContext(SuggestRequest{Input: q, ServerID: "s1", History: hist, IncludeHistory: true, Limit: 5}) {
			if it.Source == "history" || it.Source == "history_local" {
				t.Fatalf("expected dangerous/secret history denied for query %q, got %+v", q, it)
			}
		}
	}

	// 3. Command-specific case-sensitive FileFlags & non-file value flags.
	for _, noFiles := range []string{"curl -I ", "grep -i ", "journalctl -f ", "docker logs -f "} {
		if _, active := ExtractPathContextWithHome(noFiles, "/srv", "/root"); active {
			t.Fatalf("expected %q NOT to trigger local file completion", noFiles)
		}
	}
	// journalctl -f -u nginx - must NOT skip -u as an arg to -f (so -u is deduplicated!).
	jFlags := MatchFlags([]string{"journalctl", "-f", "-u", "nginx", "-"}, "-")
	for _, f := range jFlags {
		if f.Token == "-u" || f.Token == "-f" {
			t.Fatalf("expected -u and -f deduplicated in journalctl flags, got %+v", jFlags)
		}
	}
	// docker compose -p myproj up - must skip non-file value 'myproj' and resolve 'docker compose up' flags.
	dcUp := SuggestWithContext(SuggestRequest{Input: "docker compose -p myproj up -", Limit: 5})
	if len(dcUp) == 0 || dcUp[0].Source != "flag" || dcUp[0].Command != "-d" {
		t.Fatalf("expected docker compose up flags after -p myproj, got %+v", dcUp)
	}

	// 4. Case-sensitive file dedup (Makefile vs makefile) & case-fix suggestion (cat readme.md -> cat README.md).
	mkItems := SuggestWithContext(SuggestRequest{Input: "cat make", CWD: "/srv", CWDEntries: cwdEntries, Limit: 8})
	seenMK := map[string]bool{}
	for _, it := range mkItems {
		seenMK[it.Command] = true
	}
	if !seenMK["Makefile"] || !seenMK["makefile"] {
		t.Fatalf("expected both Makefile and makefile preserved without case-colliding dedup, got %+v", mkItems)
	}
	caseFix := SuggestWithContext(SuggestRequest{Input: "cat readme.md", CWD: "/srv", CWDEntries: cwdEntries, Limit: 5})
	if len(caseFix) == 0 || caseFix[0].InsertText != "cat README.md" {
		t.Fatalf("expected case-fix suggestion 'cat README.md' for 'cat readme.md', got %+v", caseFix)
	}

	// 5. EscapePathToken double-quote '!' (no \!), literal \$HOME & '$HOME' escaping, and <&0 FD redirect.
	if got := EscapePathToken("", "hi!there.txt", false, '"'); got != `"hi!there.txt"` {
		t.Fatalf("double-quoted '!' must not be backslash-escaped, got %q", got)
	}
	litEntries := []models.SFTPFileEntry{{Name: "app.conf", Size: 10}}
	litEsc := SuggestWithContext(SuggestRequest{
		Input: `cat \$HOME/a`, CWD: "/srv", HomeDir: "/home/u", TargetEntries: litEntries, Limit: 5,
	})
	if len(litEsc) == 0 || litEsc[0].InsertText != `cat \$HOME/app.conf` {
		t.Fatalf(`expected literal \$HOME/ to stay escaped, got %+v`, litEsc)
	}
	litSingle := SuggestWithContext(SuggestRequest{
		Input: `cat '$HOME'/a`, CWD: "/srv", HomeDir: "/home/u", TargetEntries: litEntries, Limit: 5,
	})
	if len(litSingle) == 0 || litSingle[0].InsertText != `cat \$HOME/app.conf` {
		t.Fatalf(`expected literal '$HOME'/ not to turn into unescaped $HOME/, got %+v`, litSingle)
	}
	pre, act := splitActiveSegment("cat <&0 app")
	if pre != "" || act != "cat <&0 app" {
		t.Fatalf("splitActiveSegment wrongly split <&0 redirection: pre=%q act=%q", pre, act)
	}
}
