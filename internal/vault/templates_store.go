package vault

import (
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
)

func (s *Store) ListTemplates() []models.BuiltinTemplate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.BuiltinTemplate, len(s.data.Templates))
	copy(out, s.data.Templates)
	return out
}

func (s *Store) SaveTemplate(tpl models.BuiltinTemplate) (models.BuiltinTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return models.BuiltinTemplate{}, errVaultClosed
	}
	if s.locked {
		return models.BuiltinTemplate{}, errVaultLocked
	}

	tpl.Name = strings.TrimSpace(tpl.Name)
	tpl.Command = strings.TrimSpace(tpl.Command)
	tpl.Category = strings.TrimSpace(tpl.Category)
	tpl.Description = strings.TrimSpace(tpl.Description)
	tpl.SafetyNote = strings.TrimSpace(tpl.SafetyNote)
	if tpl.Name == "" || tpl.Command == "" {
		return models.BuiltinTemplate{}, errors.New("模板名称与脚本内容不能为空")
	}
	if tpl.Category == "" {
		tpl.Category = "自定义"
	}

	prevTemplates := cloneTemplates(s.data.Templates)
	if tpl.ID == "" {
		tpl.ID = uuid.NewString()
		tpl.PresetID = ""
		s.data.Templates = append(s.data.Templates, tpl)
	} else {
		updated := false
		for i, existing := range s.data.Templates {
			if existing.ID == tpl.ID {
				// Preserve original preset identity so edited presets are still recognized & skipped on preset reload.
				tpl.PresetID = existing.PresetID
				s.data.Templates[i] = tpl
				updated = true
				break
			}
		}
		if !updated {
			// Prevent a newly created custom template from hijacking a built-in preset ID.
			tpl.PresetID = ""
			if builtinPresetIDSet()[tpl.ID] {
				tpl.ID = uuid.NewString()
			}
			s.data.Templates = append(s.data.Templates, tpl)
		}
	}

	if err := s.saveLocked(); err != nil {
		s.data.Templates = prevTemplates
		return models.BuiltinTemplate{}, err
	}
	return tpl, nil
}

func (s *Store) DeleteTemplate(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errVaultClosed
	}
	if s.locked {
		return errVaultLocked
	}
	found := false
	next := make([]models.BuiltinTemplate, 0, len(s.data.Templates))
	for _, tpl := range s.data.Templates {
		if tpl.ID != id {
			next = append(next, tpl)
		} else {
			found = true
		}
	}
	if !found {
		return nil
	}
	prevTemplates := cloneTemplates(s.data.Templates)
	s.data.Templates = next
	if err := s.saveLocked(); err != nil {
		s.data.Templates = prevTemplates
		return err
	}
	return nil
}

// LoadPresetTemplates appends any missing built-in preset templates without overwriting
// existing templates; any preset that already exists (even if edited) is skipped.
func (s *Store) LoadPresetTemplates() (models.PresetLoadResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return models.PresetLoadResult{}, errVaultClosed
	}
	if s.locked {
		return models.PresetLoadResult{}, errVaultLocked
	}

	existingPresets := make(map[string]bool, len(s.data.Templates)*2)
	for _, tpl := range s.data.Templates {
		if tpl.ID != "" {
			existingPresets[tpl.ID] = true
		}
		if tpl.PresetID != "" {
			existingPresets[tpl.PresetID] = true
		}
	}

	prevTemplates := cloneTemplates(s.data.Templates)
	presets := BuiltinOpsTemplates()
	added, skipped := 0, 0
	for _, preset := range presets {
		if existingPresets[preset.ID] || (preset.PresetID != "" && existingPresets[preset.PresetID]) {
			skipped++
			continue
		}
		s.data.Templates = append(s.data.Templates, preset)
		existingPresets[preset.ID] = true
		added++
	}

	if added > 0 {
		if err := s.saveLocked(); err != nil {
			s.data.Templates = prevTemplates
			return models.PresetLoadResult{}, err
		}
	}
	out := make([]models.BuiltinTemplate, len(s.data.Templates))
	copy(out, s.data.Templates)
	return models.PresetLoadResult{
		Added:     added,
		Skipped:   skipped,
		Templates: out,
	}, nil
}