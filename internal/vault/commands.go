package vault

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
)

func (s *Store) ListShortcuts() []models.CommandShortcut {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.CommandShortcut, len(s.data.Shortcuts))
	copy(out, s.data.Shortcuts)
	return out
}

func (s *Store) GetShortcut(id string) (models.CommandShortcut, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sc := range s.data.Shortcuts {
		if sc.ID == id {
			return sc, true
		}
	}
	return models.CommandShortcut{}, false
}

func (s *Store) SaveShortcut(sc models.CommandShortcut) (models.CommandShortcut, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return models.CommandShortcut{}, errVaultClosed
	}
	if s.locked {
		return models.CommandShortcut{}, errVaultLocked
	}

	sc.Name = strings.TrimSpace(sc.Name)
	sc.Command = strings.TrimSpace(sc.Command)
	if sc.Command == "" {
		return models.CommandShortcut{}, errors.New("快捷命令内容不能为空")
	}
	if sc.Name == "" {
		firstLine := strings.Split(sc.Command, "\n")[0]
		runes := []rune(firstLine)
		if len(runes) > 32 {
			firstLine = string(runes[:32]) + "..."
		}
		sc.Name = firstLine
	}
	if strings.TrimSpace(sc.Category) == "" {
		sc.Category = "常用"
	}

	now := time.Now()
	sc.UpdatedAt = now
	prevShortcuts := cloneShortcuts(s.data.Shortcuts)

	for i, existing := range s.data.Shortcuts {
		if sc.ID != "" && existing.ID == sc.ID {
			sc.CreatedAt = existing.CreatedAt
			sc.UseCount = existing.UseCount
			s.data.Shortcuts[i] = sc
			if err := s.saveLocked(); err != nil {
				s.data.Shortcuts = prevShortcuts
				return models.CommandShortcut{}, err
			}
			return sc, nil
		}
	}

	sc.ID = uuid.NewString()
	sc.CreatedAt = now
	s.data.Shortcuts = append([]models.CommandShortcut{sc}, s.data.Shortcuts...)
	if err := s.saveLocked(); err != nil {
		s.data.Shortcuts = prevShortcuts
		return models.CommandShortcut{}, err
	}
	return sc, nil
}

func (s *Store) DeleteShortcut(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errVaultClosed
	}
	if s.locked {
		return errVaultLocked
	}
	for i, sc := range s.data.Shortcuts {
		if sc.ID == id {
			prevShortcuts := cloneShortcuts(s.data.Shortcuts)
			s.data.Shortcuts = append(s.data.Shortcuts[:i], s.data.Shortcuts[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.data.Shortcuts = prevShortcuts
				return err
			}
			return nil
		}
	}
	return nil
}

func (s *Store) IncrementShortcutUse(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.locked {
		return
	}
	for i := range s.data.Shortcuts {
		if s.data.Shortcuts[i].ID == id {
			s.data.Shortcuts[i].UseCount++
			s.scheduleFlushLocked()
			return
		}
	}
}

func (s *Store) RecordCommand(serverID, serverName, sessionID, command string) (*models.CommandRecord, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errVaultClosed
	}
	if s.locked {
		return nil, errVaultLocked
	}
	if !s.data.Settings.RecordCommands {
		return nil, nil
	}

	rec := models.CommandRecord{
		ID:         uuid.NewString(),
		Command:    command,
		ServerID:   serverID,
		ServerName: serverName,
		SessionID:  sessionID,
		ExecutedAt: time.Now(),
	}

	// Tail-append in chronological order so recording is amortized O(1) without
	// copying all existing history entries or unrelated vault slices.
	s.data.History = append(s.data.History, rec)
	maxEntries := s.data.Settings.MaxHistoryEntries
	if maxEntries <= 0 {
		maxEntries = 2000
	}
	if len(s.data.History) > maxEntries {
		excess := len(s.data.History) - maxEntries
		clear(s.data.History[:excess])
		s.data.History = s.data.History[excess:]
		if cap(s.data.History) > maxEntries*2 {
			compacted := make([]models.CommandRecord, len(s.data.History), maxEntries)
			copy(compacted, s.data.History)
			s.data.History = compacted
		}
	}

	s.scheduleFlushLocked()
	return &rec, nil
}

func (s *Store) ListHistory(serverID string, limit int) []models.CommandRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 300
	}
	out := make([]models.CommandRecord, 0, min(limit, len(s.data.History)))
	for i := len(s.data.History) - 1; i >= 0; i-- {
		rec := s.data.History[i]
		if serverID != "" && rec.ServerID != serverID {
			continue
		}
		out = append(out, rec)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Store) DeleteHistoryEntry(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errVaultClosed
	}
	if s.locked {
		return errVaultLocked
	}
	for i, h := range s.data.History {
		if h.ID == id {
			prevHistory := cloneHistory(s.data.History)
			s.data.History = append(s.data.History[:i], s.data.History[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.data.History = prevHistory
				return err
			}
			return nil
		}
	}
	return nil
}

func (s *Store) ClearHistory(serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errVaultClosed
	}
	if s.locked {
		return errVaultLocked
	}
	prevHistory := cloneHistory(s.data.History)
	if serverID == "" {
		s.data.History = []models.CommandRecord{}
	} else {
		kept := make([]models.CommandRecord, 0, len(s.data.History))
		for _, h := range s.data.History {
			if h.ServerID != serverID {
				kept = append(kept, h)
			}
		}
		s.data.History = kept
	}
	if err := s.saveLocked(); err != nil {
		s.data.History = prevHistory
		return err
	}
	return nil
}

func (s *Store) GetSettings() models.AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Settings
}

func (s *Store) SaveSettings(settings models.AppSettings) (models.AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return models.AppSettings{}, errVaultClosed
	}
	if s.locked {
		return models.AppSettings{}, errVaultLocked
	}
	d := models.DefaultSettings()
	if settings.TerminalFontFamily == "" {
		settings.TerminalFontFamily = d.TerminalFontFamily
	}
	if settings.TerminalFontSize < 10 || settings.TerminalFontSize > 28 {
		settings.TerminalFontSize = d.TerminalFontSize
	}
	if settings.DefaultKeepAliveSeconds < 5 {
		settings.DefaultKeepAliveSeconds = d.DefaultKeepAliveSeconds
	}
	if settings.MaxHistoryEntries < 100 {
		settings.MaxHistoryEntries = 100
	}
	settings.SyncRelayURL = ""

	prev := s.data.Settings
	s.data.Settings = settings
	if err := s.saveLocked(); err != nil {
		s.data.Settings = prev
		return models.AppSettings{}, err
	}
	return settings, nil
}
