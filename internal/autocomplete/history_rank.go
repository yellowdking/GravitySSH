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

var secretPattern = regexp.MustCompile(`(?i)((password|passwd|token|secret|api[_-]?key)\s*=|\baws_secret_access_key\b|\bbearer\s+\S+)`)

func normWS(s string) string { return strings.Join(strings.Fields(s), " ") }

func isDestructiveSeg(seg string) bool {
	toks, _ := tokenizeSegment(seg)
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
		r, f, bad := false, false, false
		for _, a := range eff[1:] {
			switch {
			case a == "--no-preserve-root":
				return true
			case a == "--recursive":
				r = true
			case a == "--force":
				f = true
			case hp(a, "-") && !hp(a, "--"):
				r, f = r || strings.ContainsAny(a[1:], "rR"), f || ct(a[1:], "f")
			case ct(" / /* ~ ~/ $HOME ${HOME} $HOME/ . .. ", " "+a+" "):
				bad = true
			}
		}
		return r && f && bad
	case "chmod", "chown":
		rec, root := false, false
		for _, a := range eff[1:] {
			rec, root = rec || a == "-R" || a == "--recursive", root || a == "/" || a == "/*"
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
		pre, act := splitActiveSegment(rem)
		if isDestructiveSeg(act) {
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
			if isNoisyOrDestructive(norm) {
				continue
			}
			ageH := float64(i) * 2.0
			if !r.ExecutedAt.IsZero() {
				ageH = max(0.0, now.Sub(r.ExecutedAt).Hours())
			}
			a = &histAgg{cmd: norm, srv: r.ServerName, locAgeH: 1e9, anyAgeH: ageH}
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
				a.cmd, a.srv = norm, r.ServerName
			}
		}
		if srvID != "" && r.ServerID == srvID {
			a.local, a.locFreq = true, min(30, a.locFreq+1)
			if ageH < a.locAgeH {
				a.locAgeH, a.cmd, a.srv = ageH, norm, r.ServerName
			}
		}
	}

	out := make([]models.CompletionItem, 0, len(agg))
	for key, a := range agg {
		ins, qRunes := "", 0
		if strings.HasPrefix(key, loFull) {
			ins, qRunes = leadWS+a.cmd, loFullRunes
		} else if chained && strings.HasPrefix(key, loSeg) {
			if sp, _ := splitActiveSegment(a.cmd); strings.TrimSpace(sp) == "" {
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
