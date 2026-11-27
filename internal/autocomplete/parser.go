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
	PreserveLeadingEnvVar           bool
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
		} else if c == ';' || c == '|' || (c == '&' && (i == 0 || (s[i-1] != '>' && s[i-1] != '<')) && (i+1 >= len(s) || s[i+1] != '>')) {
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
				} else if q == '"' && c == '\\' && i+1 < n && strings.IndexByte("\\\"$`", s[i+1]) >= 0 {
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

func isShortFlagClusterOf(f, allowed string) bool {
	if len(f) < 2 || f[0] != '-' || f[1] == '-' {
		return false
	}
	for i := 1; i < len(f); i++ {
		if strings.IndexByte(allowed, f[i]) < 0 {
			return false
		}
	}
	return true
}

func stripWrappers(toks []Token, endSp bool) []string {
	lim := len(toks)
	if !endSp && lim > 0 {
		lim--
	}
	i := 0
	for i < lim {
		t := toks[i].Unquoted
		base := path.Base(strings.ToLower(t))
		if isEnvAssign(t) || base == "command" || base == "time" || base == "nohup" {
			i++
			continue
		}
		if base != "sudo" && base != "env" {
			break
		}
		for i++; i < lim; {
			f := toks[i].Unquoted
			if f == "--" {
				i++
				break
			}
			if base == "sudo" {
				switch {
				case isShortFlagClusterOf(f, "EHisknAbvPS") ||
					f == "--preserve-env" || f == "--set-home" || f == "--login" ||
					f == "--shell" || f == "--non-interactive" || f == "--reset-timestamp" ||
					f == "--background" || f == "--stdin":
					i++
				case strings.HasPrefix(f, "--user=") || strings.HasPrefix(f, "--group=") ||
					strings.HasPrefix(f, "--close-from=") || strings.HasPrefix(f, "--prompt=") ||
					strings.HasPrefix(f, "--type=") || strings.HasPrefix(f, "--host=") ||
					strings.HasPrefix(f, "--chdir=") || strings.HasPrefix(f, "--role=") ||
					strings.HasPrefix(f, "--preserve-env="):
					i++
				case (f == "-u" || f == "-g" || f == "-C" || f == "-p" || f == "-t" || f == "-h" || f == "-D" || f == "-r" ||
					f == "--user" || f == "--group" || f == "--close-from" || f == "--prompt" ||
					f == "--type" || f == "--host" || f == "--chdir" || f == "--role") && i+1 < lim:
					i += 2
				case len(f) >= 3 && f[0] == '-' && f[1] != '-' && strings.IndexByte("ugCpthDr", f[1]) >= 0:
					// Covers both -u=root and attached -uroot
					i++
				default:
					goto nextWrapper
				}
			} else { // env
				switch {
				case f == "-" || isShortFlagClusterOf(f, "i0") || f == "--ignore-environment" ||
					strings.HasPrefix(f, "--unset=") || strings.HasPrefix(f, "--chdir=") ||
					strings.HasPrefix(f, "-u=") || strings.HasPrefix(f, "-C="):
					i++
				case (f == "-u" || f == "-C" || f == "--unset" || f == "--chdir") && i+1 < lim:
					i += 2
				default:
					goto nextWrapper
				}
			}
		}
	nextWrapper:
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
		if !hp(cur, "./") && !hp(cur, "../") && !hp(cur, "/") && !hp(p.CurTok.Raw, "~/") {
			return p
		}
	} else {
		pre := eff
		if !endSp && len(pre) > 0 {
			pre = pre[:len(pre)-1]
		}
		pf := len(pre) >= 2 && isFileFlagForCmd(pre[:len(pre)-1], pre[len(pre)-1])
		if (SubcmdRoots[cmd] || (!FileCentricCommands[cmd] && cur == "")) && !strings.Contains(cur, "/") && !pf {
			return p
		}
		if cmd == "grep" && !pf && !strings.Contains(cur, "/") && !hasPositionalArg(pre) {
			return p
		}
		p.DirsOnly = DirOnlyCommands[cmd] && !pf
	}
	if cwd == "" {
		cwd = "/"
	}
	p.PathActive, p.NamePrefix, p.TargetDir = true, cur, cwd
	if sl := strings.LastIndexByte(cur, '/'); sl >= 0 {
		d := cur[:sl+1]
		p.TokenDir, p.NamePrefix = d, cur[sl+1:]
		raw := p.CurTok.Raw
		unescapedHomeVar := p.CurTok.Quote != '\'' && !hp(raw, `\$`) && !hp(raw, `"\$`) && !hp(raw, `'`)
		p.PreserveLeadingEnvVar = unescapedHomeVar && leadingEnvVarLen(d) > 0
		switch {
		case hp(d, "~/") && hp(raw, "~/"):
			if homeDir == "" {
				p.PathActive, p.TargetDir = false, ""
				return p
			}
			p.TargetDir = path.Clean(path.Join(homeDir, d[2:]))
		case unescapedHomeVar && (hp(d, "$HOME/") || hp(d, "${HOME}/")):
			if homeDir == "" {
				p.PathActive, p.TargetDir = false, ""
				return p
			}
			rest := d[len("$HOME/"):]
			if hp(d, "${HOME}/") {
				rest = d[len("${HOME}/"):]
			}
			p.TargetDir = path.Clean(path.Join(homeDir, rest))
		case hp(d, "/"):
			p.TargetDir = path.Clean(d)
		default:
			p.TargetDir = path.Clean(path.Join(cwd, d))
		}
	}
	return p
}

func isVarStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
}

func isVarCont(c byte) bool {
	return isVarStart(c) || (c >= '0' && c <= '9')
}

// leadingEnvVarLen returns the byte length of a leading $VAR or ${VAR} segment
// when followed by '/' or end of string, so shell variable expansion is preserved.
func leadingEnvVarLen(s string) int {
	if len(s) < 2 || s[0] != '$' {
		return 0
	}
	if s[1] == '{' {
		if len(s) < 4 || !isVarStart(s[2]) {
			return 0
		}
		i := 3
		for i < len(s) && isVarCont(s[i]) {
			i++
		}
		if i < len(s) && s[i] == '}' && (i+1 == len(s) || s[i+1] == '/') {
			return i + 1
		}
		return 0
	}
	if !isVarStart(s[1]) {
		return 0
	}
	i := 2
	for i < len(s) && isVarCont(s[i]) {
		i++
	}
	if i == len(s) || s[i] == '/' {
		return i
	}
	return 0
}

func EscapePathToken(tokenDir, name string, isDir bool, quote byte) string {
	return escapePathToken(tokenDir, name, isDir, quote, true)
}

func escapePathToken(tokenDir, name string, isDir bool, quote byte, preserveEnvVar bool) string {
	full := tokenDir + name
	if isDir {
		full += "/"
	}
	if quote == '\'' {
		s := "'" + strings.ReplaceAll(full, "'", `'\''`)
		if !isDir {
			s += "'"
		}
		return s
	}
	set := " \t'\"\\$`()[]{}*?!#&|;<>="
	var b strings.Builder
	if quote == '"' {
		b.WriteByte('"')
		set = "\\\"$`"
	}
	skip := 0
	if preserveEnvVar {
		skip = leadingEnvVarLen(tokenDir)
	}
	if skip > 0 {
		b.WriteString(full[:skip])
	}
	for i := skip; i < len(full); i++ {
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
