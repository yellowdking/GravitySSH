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
	sc := []models.CommandShortcut{{Name: "部署", Command: "docker compose up -d", Pinned: true}}
	for _, it := range Suggest("docker ", sc, nil, true, false, 8) {
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
	if _, ok := ExtractPathContext("docker c", "/srv"); ok {
		t.Fatal("suppress")
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
		mk("rm -rf /", "s1", 0), mk("sudo rm -rf /*", "s1", 0),
		mk("TOKEN=abc curl https://s.io", "s1", 0), mk("mkfs.ext4 /dev/sda1", "s1", 0), mk("reboot", "s1", 0),
	}
	hq := func(in string) []models.CompletionItem {
		return SuggestWithContext(SuggestRequest{Input: in, ServerID: "s1", History: hist, IncludeHistory: true, Limit: 5})
	}
	if it := hq("curl https://"); len(it) != 2 || it[0].Source != "history_local" || it[0].Category != "本机历史" || it[1].Source != "history" {
		t.Fatalf("hist: %+v", it)
	}
	for _, q := range []string{"rm -r", "sudo rm", "mkfs", "rebo", "TOKEN="} {
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
