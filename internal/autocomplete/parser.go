package autocomplete

import (
	"path"
	"strings"
)

type Token struct {
	Raw, Unquoted string
	Start, End    int
	Quote         byte
}

type ParsedLine struct {
	SegPrefix, ActiveSeg, TokPrefix string
	Tokens                          []Token
	Effective                       []string
	CurTok                          Token
	PrevTok                         string
	EndsWithSpace, PathActive       bool
	TargetDir, TokenDir, NamePrefix string
	DirsOnly                        bool
}

func splitActiveSegment(s string) (string, string) {
	var q byte
	esc, last := false, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if esc {
			esc = false
		} else if q != 0 {
			if q == '"' && c == '\\' {
				esc = true
			} else if c == q {
				q = 0
			}
		} else if c == '\\' {
			esc = true
		} else if c == '\'' || c == '"' {
			q = c
		} else if (c == '&' || c == '|') && i+1 < len(s) && (s[i+1] == c || (c == '|' && s[i+1] == '&')) {
			last, i = i+2, i+1
		} else if c == ';' || c == '|' {
			last = i + 1
		}
	}
	for last < len(s) && (s[last] == ' ' || s[last] == '\t') {
		last++
	}
	return s[:last], s[last:]
}

func tokenizeSegment(s string) ([]Token, bool) {
	var toks []Token
	for i, n := 0, len(s); i < n; {
		for i < n && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= n {
			return toks, true
		}
		st, q := i, byte(0)
		var b strings.Builder
		for ; i < n; i++ {
			c := s[i]
			if q != 0 {
				if c == q {
					q = 0
				} else if q == '"' && c == '\\' && i+1 < n && strings.IndexByte(`\"$`+"`!", s[i+1]) >= 0 {
					i++
					b.WriteByte(s[i])
				} else {
					b.WriteByte(c)
				}
			} else if c == ' ' || c == '\t' {
				break
			} else if c == '\'' || c == '"' {
				q = c
			} else if c == '\\' {
				if i+1 < n {
					i++
					b.WriteByte(s[i])
				}
			} else {
				b.WriteByte(c)
			}
		}
		toks = append(toks, Token{s[st:i], b.String(), st, i, q})
	}
	return toks, false
}

func isEnvAssign(s string) bool {
	eq := strings.IndexByte(s, '=')
	for i := 0; i < eq; i++ {
		c := s[i]
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_' || (i > 0 && c >= '0' && c <= '9')) {
			return false
		}
	}
	return eq > 0
}

func stripWrappers(toks []Token, endSp bool) []string {
	lim := len(toks)
	if !endSp && lim > 0 {
		lim--
	}
	i := 0
	for i < lim {
		t, low := toks[i].Unquoted, strings.ToLower(toks[i].Unquoted)
		if isEnvAssign(t) || low == "command" || low == "time" || low == "nohup" {
			i++
			continue
		}
		if low != "sudo" && low != "env" {
			break
		}
		bf, vf := " -E -H -i -s -n -k -A -b -v -P -S ", " -u -g -C -p -t -h -D -r "
		if low == "env" {
			bf, vf = " -i -0 --ignore-environment - ", " -u -C --unset --chdir "
		}
		for i++; i < lim; {
			f := toks[i].Unquoted
			if f == "--" {
				i++
				break
			}
			if strings.Contains(bf, " "+f+" ") || (low == "env" && (strings.HasPrefix(f, "--unset=") || strings.HasPrefix(f, "--chdir="))) {
				i++
			} else if strings.Contains(vf, " "+f+" ") && i+1 < lim {
				i += 2
			} else {
				break
			}
		}
	}
	eff := make([]string, len(toks)-i)
	for j, t := range toks[i:] {
		eff[j] = t.Unquoted
	}
	return eff
}

func ParseLine(input, cwd, homeDir string) ParsedLine {
	hp, lo := strings.HasPrefix, strings.ToLower
	segP, act := splitActiveSegment(input)
	toks, endSp := tokenizeSegment(act)
	eff := stripWrappers(toks, endSp)
	p := ParsedLine{SegPrefix: segP, ActiveSeg: act, Tokens: toks, Effective: eff, EndsWithSpace: endSp}
	if n := len(toks); endSp || n == 0 {
		p.TokPrefix, p.CurTok = segP+act, Token{Start: len(act), End: len(act)}
		if n > 0 {
			p.PrevTok = toks[n-1].Unquoted
		}
	} else {
		p.CurTok, p.TokPrefix = toks[n-1], segP+act[:toks[n-1].Start]
		if n >= 2 {
			p.PrevTok = toks[n-2].Unquoted
		}
	}
	cur := p.CurTok.Unquoted
	if strings.TrimSpace(act) == "" || (hp(cur, "-") && !strings.Contains(cur, "/")) {
		return p
	}
	cmd := ""
	if len(eff) > 0 {
		cmd = lo(eff[0])
	}
	if len(eff) == 0 || (len(eff) == 1 && !endSp) {
		if !hp(cur, "./") && !hp(cur, "../") && !hp(cur, "/") && !hp(cur, "~/") {
			return p
		}
	} else {
		pf := FileFlags[lo(p.PrevTok)]
		if (SubcmdRoots[cmd] || (!FileCentricCommands[cmd] && cur == "")) && !strings.Contains(cur, "/") && !pf {
			return p
		}
		p.DirsOnly = DirOnlyCommands[cmd] && !pf
	}
	if cwd == "" {
		cwd = "/"
	}
	if homeDir == "" {
		homeDir = "/"
	}
	p.PathActive, p.NamePrefix, p.TargetDir = true, cur, cwd
	if sl := strings.LastIndexByte(cur, '/'); sl >= 0 {
		d := cur[:sl+1]
		p.TokenDir, p.NamePrefix = d, cur[sl+1:]
		if hp(d, "~/") {
			p.TargetDir = path.Clean(path.Join(homeDir, d[2:]))
		} else if hp(d, "/") {
			p.TargetDir = path.Clean(d)
		} else {
			p.TargetDir = path.Clean(path.Join(cwd, d))
		}
	}
	return p
}

func EscapePathToken(tokenDir, name string, isDir bool, quote byte) string {
	full := tokenDir + name
	if isDir {
		full += "/"
	}
	set := " \t'\"\\$`()[]{}*?!#&|;<>="
	if quote == '\'' {
		s := "'" + strings.ReplaceAll(full, "'", `'\''`)
		if !isDir {
			s += "'"
		}
		return s
	}
	var b strings.Builder
	if quote == '"' {
		b.WriteByte('"')
		set = `\"$` + "`!"
	}
	for i := 0; i < len(full); i++ {
		if strings.IndexByte(set, full[i]) >= 0 {
			b.WriteByte('\\')
		}
		b.WriteByte(full[i])
	}
	if quote == '"' && !isDir {
		b.WriteByte('"')
	}
	return b.String()
}

func ComputeCompletionEdit(curLine, tgtLine string) (int, string) {
	a, b, c := []rune(curLine), []rune(tgtLine), 0
	for c < len(a) && c < len(b) && a[c] == b[c] {
		c++
	}
	return len(a) - c, string(b[c:])
}
