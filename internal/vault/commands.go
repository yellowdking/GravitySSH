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

func (s *Store) SaveShortcut(sc models.CommandShortcut) (models.CommandShortcut, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sc.Name = strings.TrimSpace(sc.Name)
	sc.Command = strings.TrimSpace(sc.Command)
	if sc.Command == "" {
		return models.CommandShortcut{}, errors.New("快捷命令内容不能为空")
	}
	if sc.Name == "" {
		firstLine := strings.Split(sc.Command, "\n")[0]
		if len(firstLine) > 32 {
			firstLine = firstLine[:32] + "..."
		}
		sc.Name = firstLine
	}
	if strings.TrimSpace(sc.Category) == "" {
		sc.Category = "常用"
	}

	now := time.Now()
	sc.UpdatedAt = now

	for i, existing := range s.data.Shortcuts {
		if sc.ID != "" && existing.ID == sc.ID {
			sc.CreatedAt = existing.CreatedAt
			sc.UseCount = existing.UseCount
			s.data.Shortcuts[i] = sc
			if err := s.saveLocked(); err != nil {
				return models.CommandShortcut{}, err
			}
			return sc, nil
		}
	}

	sc.ID = uuid.NewString()
	sc.CreatedAt = now
	s.data.Shortcuts = append([]models.CommandShortcut{sc}, s.data.Shortcuts...)
	if err := s.saveLocked(); err != nil {
		return models.CommandShortcut{}, err
	}
	return sc, nil
}

func (s *Store) DeleteShortcut(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sc := range s.data.Shortcuts {
		if sc.ID == id {
			s.data.Shortcuts = append(s.data.Shortcuts[:i], s.data.Shortcuts[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

func (s *Store) IncrementShortcutUse(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Shortcuts {
		if s.data.Shortcuts[i].ID == id {
			s.data.Shortcuts[i].UseCount++
			_ = s.saveLocked()
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

	s.data.History = append([]models.CommandRecord{rec}, s.data.History...)
	maxEntries := s.data.Settings.MaxHistoryEntries
	if maxEntries <= 0 {
		maxEntries = 2000
	}
	if len(s.data.History) > maxEntries {
		s.data.History = s.data.History[:maxEntries]
	}

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *Store) ListHistory(serverID string, limit int) []models.CommandRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 300
	}
	out := make([]models.CommandRecord, 0, min(limit, len(s.data.History)))
	for _, rec := range s.data.History {
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
	for i, h := range s.data.History {
		if h.ID == id {
			s.data.History = append(s.data.History[:i], s.data.History[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

func (s *Store) ClearHistory(serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return s.saveLocked()
}

func (s *Store) GetSettings() models.AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Settings
}

func (s *Store) SaveSettings(settings models.AppSettings) (models.AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	if settings.SFTPPanelPosition != "right" && settings.SFTPPanelPosition != "bottom" {
		settings.SFTPPanelPosition = "right"
	}
	if settings.Language != "zh" && settings.Language != "en" {
		settings.Language = "zh"
	}
	settings.SyncRelayURL = strings.TrimSpace(settings.SyncRelayURL)

	s.data.Settings = settings
	if err := s.saveLocked(); err != nil {
		return models.AppSettings{}, err
	}
	return settings, nil
}
