package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/yellowdking/GravitySSH/internal/autocomplete"
	"github.com/yellowdking/GravitySSH/internal/models"
)

func (a *App) ListCommandHistory(serverID string, limit int) ([]models.CommandRecord, error) {
	store, _, _, err := a.readyDeps()
	if err != nil {
		return nil, err
	}
	return store.ListHistory(serverID, limit), nil
}

func (a *App) DeleteHistoryEntry(id string) error {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return store.DeleteHistoryEntry(id)
}

func (a *App) ClearHistory(serverID string) error {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return store.ClearHistory(serverID)
}

func (a *App) ListShortcuts() ([]models.CommandShortcut, error) {
	store, _, _, err := a.readyDeps()
	if err != nil {
		return nil, err
	}
	return store.ListShortcuts(), nil
}

func (a *App) ListOpsTemplates() ([]models.BuiltinTemplate, error) {
	store, _, _, err := a.readyDeps()
	if err != nil {
		return nil, err
	}
	return store.ListTemplates(), nil
}

func (a *App) SaveOpsTemplate(tpl models.BuiltinTemplate) (models.BuiltinTemplate, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.BuiltinTemplate{}, err
	}
	return store.SaveTemplate(tpl)
}

func (a *App) DeleteOpsTemplate(id string) error {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return store.DeleteTemplate(id)
}

func (a *App) LoadPresetTemplates() (models.PresetLoadResult, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.PresetLoadResult{}, err
	}
	return store.LoadPresetTemplates()
}

func (a *App) SaveShortcut(sc models.CommandShortcut) (models.CommandShortcut, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.CommandShortcut{}, err
	}
	return store.SaveShortcut(sc)
}

func (a *App) DeleteShortcut(id string) error {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return store.DeleteShortcut(id)
}

// stripBracketedPasteMarkers removes bracketed-paste start/end escape sequences
// (both 7-bit CSI and 8-bit C1) repeatedly until stable so nested payloads cannot
// reconstruct a breakout sequence.
func stripBracketedPasteMarkers(s string) string {
	replacer := strings.NewReplacer(
		"\x1b[200~", "",
		"\x1b[201~", "",
		"\x9b200~", "",
		"\x9b201~", "",
	)
	for {
		next := replacer.Replace(s)
		if next == s {
			return s
		}
		s = next
	}
}

// formatShortcutFillInput prepares a shortcut command for fill-only (non-executing)
// terminal insertion: normalizes line endings, strips trailing newlines so it never
// submits, and wraps multi-line payloads in sanitized bracketed-paste sequences so
// lines 1..N-1 cannot silently execute.
func formatShortcutFillInput(raw string) string {
	norm := strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
	norm = strings.TrimRight(norm, "\n")
	if norm == "" {
		return ""
	}
	if !strings.Contains(norm, "\n") {
		return norm
	}
	safe := stripBracketedPasteMarkers(norm)
	if safe == "" {
		return ""
	}
	return "\x1b[200~" + safe + "\x1b[201~"
}

type heredocSpec struct {
	delim     string
	stripTabs bool
}

// extractHeredocSpecs scans a shell command line for unquoted heredoc operators
// (<<DELIM, <<'DELIM', <<"DELIM", <<\DELIM, <<-DELIM) while ignoring <<< here-strings,
// quoted strings, and comments.
func extractHeredocSpecs(line string) []heredocSpec {
	var specs []heredocSpec
	var q byte
	esc := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if esc {
			esc = false
			continue
		}
		if q != 0 {
			if q == '"' && c == '\\' {
				esc = true
			} else if c == q {
				q = 0
			}
			continue
		}
		switch c {
		case '\\':
			esc = true
		case '\'', '"':
			q = c
		case '#':
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' || line[i-1] == ';' {
				return specs
			}
		case '<':
			if i+1 < len(line) && line[i+1] == '<' {
				// Skip <<< here-string
				if i+2 < len(line) && line[i+2] == '<' {
					i += 2
					continue
				}
				j := i + 2
				stripTabs := false
				if j < len(line) && line[j] == '-' {
					stripTabs = true
					j++
				}
				for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
					j++
				}
				var d strings.Builder
				var dq byte
				desc := false
				for j < len(line) {
					ch := line[j]
					if desc {
						d.WriteByte(ch)
						desc = false
						j++
						continue
					}
					if dq != 0 {
						if dq == '"' && ch == '\\' {
							desc = true
							j++
							continue
						}
						if ch == dq {
							dq = 0
							j++
							continue
						}
						d.WriteByte(ch)
						j++
						continue
					}
					if ch == '\\' {
						desc = true
						j++
						continue
					}
					if ch == '\'' || ch == '"' {
						dq = ch
						j++
						continue
					}
					if strings.IndexByte(" \t;|&<>()", ch) >= 0 {
						break
					}
					d.WriteByte(ch)
					j++
				}
				if delim := d.String(); delim != "" {
					specs = append(specs, heredocSpec{delim: delim, stripTabs: stripTabs})
				}
				i = j - 1
			}
		}
	}
	return specs
}

// splitShortcutExecLines normalizes line endings and drops empty lines outside
// heredocs while preserving blank lines inside heredoc bodies verbatim.
func splitShortcutExecLines(command string) []string {
	norm := strings.ReplaceAll(strings.ReplaceAll(command, "\r\n", "\n"), "\r", "\n")
	var (
		lines    []string
		heredocs []heredocSpec
	)
	for _, line := range strings.Split(norm, "\n") {
		if len(heredocs) > 0 {
			lines = append(lines, line)
			candidate := line
			if heredocs[0].stripTabs {
				candidate = strings.TrimLeft(line, "\t")
			}
			if candidate == heredocs[0].delim {
				heredocs = heredocs[1:]
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
		if specs := extractHeredocSpecs(line); len(specs) > 0 {
			heredocs = append(heredocs, specs...)
		}
	}
	return lines
}

type sessionShortcutRunner struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func (r *sessionShortcutRunner) cancelAndWaitLocked() {
	if r.cancel != nil {
		r.cancel()
		if r.done != nil {
			<-r.done
		}
		r.cancel = nil
		r.done = nil
	}
}

func (r *sessionShortcutRunner) cancelAndWait() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelAndWaitLocked()
}

func (r *sessionShortcutRunner) run(
	ctxDone <-chan struct{},
	getStopCh func() <-chan struct{},
	command string,
	execute bool,
	writeInput func(string) error,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !execute {
		fill := formatShortcutFillInput(command)
		if fill == "" {
			return nil
		}
		return writeInput(fill)
	}

	// Cancel any prior in-flight paced script on this session so lines never interleave.
	r.cancelAndWaitLocked()

	lines := splitShortcutExecLines(command)
	if len(lines) == 0 {
		return nil
	}
	if err := writeInput(lines[0] + "\r"); err != nil || len(lines) == 1 {
		return err
	}
	var stopCh <-chan struct{}
	if getStopCh != nil {
		stopCh = getStopCh()
	}
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.cancel = cancel
	r.done = done

	rest := lines[1:]
	go func() {
		defer close(done)
		defer cancel()
		timer := time.NewTimer(35 * time.Millisecond)
		defer timer.Stop()
		for i, line := range rest {
			if i > 0 {
				timer.Reset(35 * time.Millisecond)
			}
			select {
			case <-runCtx.Done():
				return
			case <-ctxDone:
				return
			case <-stopCh:
				return
			case <-timer.C:
			}
			if err := writeInput(line + "\r"); err != nil {
				return
			}
		}
	}()
	return nil
}

func (a *App) getShortcutRunner(sessionID string) *sessionShortcutRunner {
	a.shortcutMu.Lock()
	defer a.shortcutMu.Unlock()
	if a.shortcutRunners == nil {
		a.shortcutRunners = make(map[string]*sessionShortcutRunner)
	}
	r, ok := a.shortcutRunners[sessionID]
	if !ok {
		r = &sessionShortcutRunner{}
		a.shortcutRunners[sessionID] = r
	}
	return r
}

func (a *App) cancelShortcutRun(sessionID string) {
	a.shortcutMu.Lock()
	r := a.shortcutRunners[sessionID]
	delete(a.shortcutRunners, sessionID)
	a.shortcutMu.Unlock()
	if r != nil {
		r.cancelAndWait()
	}
}

func (a *App) cancelAllShortcutRuns() {
	a.shortcutMu.Lock()
	runners := make([]*sessionShortcutRunner, 0, len(a.shortcutRunners))
	for _, r := range a.shortcutRunners {
		runners = append(runners, r)
	}
	clear(a.shortcutRunners)
	a.shortcutMu.Unlock()
	for _, r := range runners {
		r.cancelAndWait()
	}
}

func runShortcutPayload(
	ctxDone <-chan struct{},
	getStopCh func() <-chan struct{},
	command string,
	execute bool,
	writeInput func(string) error,
) error {
	var runner sessionShortcutRunner
	return runner.run(ctxDone, getStopCh, command, execute, writeInput)
}

// RunShortcut fills or executes single- or multi-line command scripts (35ms pacing for multi-line).
// Calling RunShortcut with execute=false NEVER executes, regardless of shortcut.AutoExecute.
func (a *App) RunShortcut(sessionID, shortcutID string, execute bool) error {
	store, sshMgr, ctx, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	target, ok := store.GetShortcut(shortcutID)
	if !ok {
		return errors.New("快捷命令不存在")
	}
	store.IncrementShortcutUse(shortcutID)

	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}
	runner := a.getShortcutRunner(sessionID)
	return runner.run(
		ctxDone,
		func() <-chan struct{} { return sshMgr.SessionStopCh(sessionID) },
		target.Command,
		execute,
		func(data string) error { return sshMgr.WriteInput(sessionID, data) },
	)
}

func (a *App) SuggestCommandsForSession(input, serverID, sessionID string) ([]models.CompletionItem, error) {
	store, sshMgr, _, err := a.readyDeps()
	if err != nil {
		return nil, err
	}
	settings := store.GetSettings()
	if !settings.AutocompleteEnabled {
		return []models.CompletionItem{}, nil
	}

	var cwd, homeDir string
	var cwdEntries, targetEntries []models.SFTPFileEntry
	var parsed autocomplete.ParsedLine
	if sessionID != "" {
		cwd = sshMgr.GetSessionCWD(sessionID)
		homeDir = sshMgr.GetSessionHomeDir(sessionID)
		cwdEntries = sshMgr.PeekCachedDirEntries(sessionID, cwd)
	}
	parsed = autocomplete.ParseLine(input, cwd, homeDir)
	if sessionID != "" && parsed.PathActive {
		if parsed.TargetDir == cwd {
			targetEntries = cwdEntries
		} else {
			targetEntries = sshMgr.PeekCachedDirEntries(sessionID, parsed.TargetDir)
		}
	}

	var history []models.CommandRecord
	if settings.AutocompleteFromHistory {
		history = store.ListHistory(serverID, 150)
		if serverID != "" && len(history) < 200 {
			for _, rec := range store.ListHistory("", 200) {
				if rec.ServerID == serverID {
					continue
				}
				history = append(history, rec)
				if len(history) >= 200 {
					break
				}
			}
		}
	}

	items := autocomplete.SuggestWithContext(autocomplete.SuggestRequest{
		Input: input, CWD: cwd, HomeDir: homeDir, ServerID: serverID,
		Parsed: &parsed, CWDEntries: cwdEntries, TargetEntries: targetEntries,
		History: history, IncludeHistory: settings.AutocompleteFromHistory, Limit: 5,
	})
	if items == nil {
		return []models.CompletionItem{}, nil
	}
	return items, nil
}

func (a *App) GetSettings() (models.AppSettings, error) {
	store, _, _, err := a.readyDeps()
	if err != nil {
		return models.AppSettings{}, err
	}
	return store.GetSettings(), nil
}

func (a *App) SaveSettings(settings models.AppSettings) (models.AppSettings, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.AppSettings{}, err
	}
	return store.SaveSettings(settings)
}