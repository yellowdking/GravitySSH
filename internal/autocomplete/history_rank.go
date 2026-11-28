package autocomplete

import (
	"math"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yellowdking/GravitySSH/internal/models"
)

type histAgg struct {
	cmd, srv         string
	local            bool
	locFreq, allFreq int
	locAgeH, anyAgeH float64
}

var secretPattern = regexp.MustCompile(`(?i)((password|passwd|token|secret|api[_-]?key)\s*=|\baws_secret_access_key\b|\bbearer\s+\S+|\bauthorization\s*:\s*basic\s+\S+|://[^/\s:@]+:[^/\s@]+@|\bsshpass\b[^|;&]*(?:^|\s)(?:-p\s*\S+|--password[=\s]\S+)|\bcurl\b[^|;&]*(?:^|\s)(?:-u\s*\S+:\S+|--user[=\s]\S+:\S+))`)

// normWS collapses unquoted whitespace runs to a single space while preserving
// spaces/tabs inside single quotes, double quotes, and backslash-escaped spaces.
func normWS(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	var q byte
	esc, pendingSpace := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if esc {
			if pendingSpace && b.Len() > 0 {
				b.WriteByte(' ')
				pendingSpace = false
			}
			b.WriteByte(c)
			esc = false
			continue
		}
		if q != 0 {
			if q == '"' && c == '\\' {
				esc = true
			} else if c == q {
				q = 0
			}
			b.WriteByte(c)
			continue
		}
		if c == ' ' || c == '\t' {
			pendingSpace = true
			continue
		}
		if pendingSpace && b.Len() > 0 {
			b.WriteByte(' ')
		}
		pendingSpace = false
		if c == '\\' {
			esc = true
		} else if c == '\'' || c == '"' {
			q = c
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isRootOrWildcardRoot(a string) bool {
	if a == "" {
		return false
	}
	if strings.HasSuffix(a, "/*") || strings.HasSuffix(a, "/*/") {
		base := strings.TrimSuffix(strings.TrimSuffix(a, "/"), "/*")
		if base == "" {
			return true
		}
		if path.Clean(base) == "/" {
			return true
		}
	}
	return path.Clean(a) == "/"
}

func isDangerousRMTarget(a string) bool {
	if a == "" {
		return false
	}
	if isRootOrWildcardRoot(a) {
		return true
	}
	// Check home/tilde targets with any number of slashes or trailing glob (e.g. ~, ~/, ~/*, ~//*, $HOME/*, ${HOME}/*)
	trimmedSlash := strings.TrimRight(a, "/")
	for _, home := range []string{"~", "$HOME", "${HOME}"} {
		if trimmedSlash == home {
			return true
		}
		if strings.HasPrefix(a, home+"/") {
			rest := strings.TrimLeft(a[len(home):], "/")
			rest = strings.TrimRight(rest, "/")
			if rest == "" || rest == "*" || rest == "." || rest == "./*" {
				return true
			}
		}
	}
	// Check . / .. / ./* / ../*
	cleaned := path.Clean(a)
	if cleaned == "." || cleaned == ".." {
		return true
	}
	if strings.HasSuffix(a, "/*") || strings.HasSuffix(a, "/*/") {
		base := strings.TrimSuffix(strings.TrimSuffix(a, "/"), "/*")
		if c := path.Clean(base); c == "." || c == ".." {
			return true
		}
	}
	return false
}

func isDestructiveTokens(toks []Token) bool {
	eff := stripWrappers(toks, true)
	if len(eff) == 0 {
		return false
	}
	c0, ct, hp := path.Base(strings.ToLower(eff[0])), strings.Contains, strings.HasPrefix
	if hp(c0, "mkfs") || ct(" wipefs fdisk parted shred shutdown reboot poweroff halt ", " "+c0+" ") ||
		(c0 == "init" && len(eff) > 1 && (eff[1] == "0" || eff[1] == "6")) {
		return true
	}
	switch c0 {
	case "dd":
		for _, a := range eff[1:] {
			loA := strings.ToLower(a)
			if hp(loA, "if=") || hp(loA, "of=") {
				return true
			}
		}
	case "rm":
		r, f, bad, afterDD := false, false, false, false
		for _, a := range eff[1:] {
			if !afterDD && a == "--" {
				afterDD = true
				continue
			}
			switch {
			case !afterDD && a == "--no-preserve-root":
				return true
			case !afterDD && a == "--recursive":
				r = true
			case !afterDD && a == "--force":
				f = true
			case !afterDD && hp(a, "-") && !hp(a, "--"):
				r, f = r || strings.ContainsAny(a[1:], "rR"), f || ct(a[1:], "f")
			case isDangerousRMTarget(a):
				bad = true
			}
		}
		return r && f && bad
	case "chmod", "chown":
		rec, root := false, false
		for _, a := range eff[1:] {
			rec = rec || a == "-R" || a == "--recursive"
			if isRootOrWildcardRoot(a) {
				root = true
			}
		}
		return rec && root
	}
	return false
}

func isNoisyOrDestructive(cmd string) bool {
	ct, hp, lo := strings.Contains, strings.HasPrefix, strings.ToLower(cmd)
	if cmd == "" || utf8.RuneCountInString(cmd) > 160 || strings.ContainsAny(cmd, "\r\n") || ct(lo, ":(){") ||
		secretPattern.MatchString(cmd) {
		return true
	}
	for r := lo; ; {
		i := strings.IndexByte(r, '>')
		if i < 0 {
			break
		}
		r = strings.TrimLeft(r[i+1:], "> \t")
		if hp(r, "/dev/sd") || hp(r, "/dev/nvme") || hp(r, "/dev/vd") {
			return true
		}
	}
	for rem := cmd; rem != ""; {
		pre, _, toks, _ := parseActiveSegmentTokens(rem)
		if isDestructiveTokens(toks) {
			return true
		}
		nxt := strings.TrimRight(strings.TrimRight(pre, " \t"), "&|;")
		if len(nxt) >= len(rem) {
			break
		}
		rem = nxt
	}
	return false
}

func rankHistoryCandidates(p ParsedLine, input, srvID string, history []models.CommandRecord, limit int) []models.CompletionItem {
	if len(history) == 0 || strings.TrimSpace(p.ActiveSeg) == "" ||
		(len(p.Effective) == 1 && !p.EndsWithSpace && utf8.RuneCountInString(p.CurTok.Unquoted) < 2 && !p.PathActive) {
		return nil
	}
	if limit <= 0 {
		limit = 5
	}
	now, trimIn := time.Now(), strings.TrimLeft(input, " \t")
	leadWS, fullQ, segQ := input[:len(input)-len(trimIn)], normWS(trimIn), normWS(p.ActiveSeg)
	if p.EndsWithSpace {
		fullQ, segQ = fullQ+" ", segQ+" "
	}
	loFull, loSeg, chained := strings.ToLower(fullQ), strings.ToLower(segQ), strings.TrimSpace(p.SegPrefix) != ""
	loFullRunes, loSegRunes := utf8.RuneCountInString(loFull), utf8.RuneCountInString(loSeg)

	agg := make(map[string]*histAgg, min(len(history), 32))
	for i, r := range history {
		raw := strings.TrimSpace(r.Command)
		if raw == "" || strings.ContainsAny(raw, "\r\n") || utf8.RuneCountInString(raw) > 160 {
			continue
		}
		norm := normWS(raw)
		key := strings.ToLower(norm)
		// Prefix-filter before running heavier destructive/secret inspection.
		if !strings.HasPrefix(key, loFull) && !(chained && strings.HasPrefix(key, loSeg)) {
			continue
		}
		a := agg[key]
		if a == nil {
			if isNoisyOrDestructive(raw) {
				continue
			}
			ageH := float64(i) * 2.0
			if !r.ExecutedAt.IsZero() {
				ageH = max(0.0, now.Sub(r.ExecutedAt).Hours())
			}
			a = &histAgg{cmd: raw, srv: r.ServerName, locAgeH: 1e9, anyAgeH: ageH}
			agg[key] = a
		}
		ageH := float64(i) * 2.0
		if !r.ExecutedAt.IsZero() {
			ageH = max(0.0, now.Sub(r.ExecutedAt).Hours())
		}
		a.allFreq = min(30, a.allFreq+1)
		if ageH < a.anyAgeH {
			a.anyAgeH = ageH
			if !a.local {
				a.cmd, a.srv = raw, r.ServerName
			}
		}
		if srvID != "" && r.ServerID == srvID {
			a.local, a.locFreq = true, min(30, a.locFreq+1)
			if ageH < a.locAgeH {
				a.locAgeH, a.cmd, a.srv = ageH, raw, r.ServerName
			}
		}
	}

	out := make([]models.CompletionItem, 0, len(agg))
	for key, a := range agg {
		ins, qRunes := "", 0
		if strings.HasPrefix(key, loFull) {
			ins, qRunes = leadWS+a.cmd, loFullRunes
		} else if chained && strings.HasPrefix(key, loSeg) {
			if sp, _, _, _ := parseActiveSegmentTokens(a.cmd); strings.TrimSpace(sp) == "" {
				ins, qRunes = p.SegPrefix+a.cmd, loSegRunes
			}
		}
		if ins == "" || strings.EqualFold(strings.TrimSpace(ins), strings.TrimSpace(input)) {
			continue
		}
		freq, ageH := a.allFreq, a.anyAgeH
		if a.local {
			freq, ageH = a.locFreq, a.locAgeH
		}
		cmdRunes := utf8.RuneCountInString(a.cmd)
		raw := int(math.Round(100*math.Exp(-ageH/72))) + min(60, int(math.Round(20*math.Log(float64(1+freq))))) + 90 - min(max(0, cmdRunes-qRunes), 60)
		src, cat, desc, sc := "history", "历史", "历史", min(370, max(250, 250+int(math.Round(float64(raw)*120/250))))
		if a.local {
			src, cat, desc, sc = "history_local", "本机历史", "本机历史", min(495, max(400, 400+int(math.Round(float64(raw)*95/250))))
		}
		if a.srv != "" {
			desc += " · " + a.srv
		}
		out = append(out, mkItem(a.cmd, desc, cat, src, ins, sc))
	}
	return sortTopItems(out, limit)
}
