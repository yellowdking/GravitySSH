package vault

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yellowdking/GravitySSH/internal/models"
)

func TestBuiltinOpsTemplatesCompleteAndSafe(t *testing.T) {
	tpls := BuiltinOpsTemplates()
	if len(tpls) != 5 {
		t.Fatalf("expected 5 curated VPS templates, got %d", len(tpls))
	}
	wantIDs := map[string]bool{
		"tpl-bbr": false, "tpl-swap": false, "tpl-mirror": false, "tpl-docker": false, "tpl-ssh-port": false,
	}
	for _, tpl := range tpls {
		if _, ok := wantIDs[tpl.ID]; !ok {
			t.Fatalf("unexpected template ID: %s", tpl.ID)
		}
		if tpl.PresetID != tpl.ID {
			t.Fatalf("expected PresetID == ID for %s, got %q", tpl.ID, tpl.PresetID)
		}
		wantIDs[tpl.ID] = true
		if strings.TrimSpace(tpl.Name) == "" || strings.TrimSpace(tpl.Command) == "" {
			t.Fatalf("template %s has empty name or command", tpl.ID)
		}
		if tpl.ID == "tpl-ssh-port" && (!strings.Contains(tpl.Command, "sshd -t") || !strings.Contains(tpl.Command, "600")) {
			t.Fatal("ssh-port template must validate sshd -t and include rollback safeguard")
		}
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Fatalf("missing template %s", id)
		}
	}
}

func TestTemplatesCRUDAndNonOverwritingPresetReload(t *testing.T) {
	vaultFile := filepath.Join(t.TempDir(), "vault.enc")
	s, err := Open(vaultFile, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	if got := len(s.ListTemplates()); got != 5 {
		t.Fatalf("expected 5 initial templates, got %d", got)
	}

	// 1. Add custom template
	custom, err := s.SaveTemplate(models.BuiltinTemplate{
		Name: "清理日志", Category: "自定义", Description: "清理 journald", Command: "sudo journalctl --vacuum-time=3d",
	})
	if err != nil || custom.ID == "" || custom.PresetID != "" {
		t.Fatalf("save custom template failed: %+v err=%v", custom, err)
	}

	// 2. Edit an existing preset template (must preserve PresetID & NOT be overwritten on preset reload)
	edited, err := s.SaveTemplate(models.BuiltinTemplate{
		ID: "tpl-bbr", Name: "我的 BBR 配置", Category: "网络优化", Command: "sysctl -p",
	})
	if err != nil || edited.PresetID != "tpl-bbr" {
		t.Fatalf("edit preset failed: %+v err=%v", edited, err)
	}

	// 3. Delete one preset template
	if err := s.DeleteTemplate("tpl-swap"); err != nil {
		t.Fatalf("delete preset failed: %v", err)
	}

	// 4. LoadPresetTemplates: restores ONLY tpl-swap, skips the other 4 (including edited tpl-bbr), keeps custom
	res, err := s.LoadPresetTemplates()
	if err != nil {
		t.Fatalf("LoadPresetTemplates failed: %v", err)
	}
	if res.Added != 1 || res.Skipped != 4 || len(res.Templates) != 6 {
		t.Fatalf("unexpected reload result: added=%d skipped=%d total=%d", res.Added, res.Skipped, len(res.Templates))
	}

	foundCustom, foundEditedBBR, foundRestoredSwap := false, false, false
	for _, tpl := range res.Templates {
		if tpl.ID == custom.ID && tpl.Name == "清理日志" {
			foundCustom = true
		}
		if tpl.ID == "tpl-bbr" && tpl.Name == "我的 BBR 配置" && tpl.Command == "sysctl -p" {
			foundEditedBBR = true
		}
		if tpl.ID == "tpl-swap" {
			foundRestoredSwap = true
		}
	}
	if !foundCustom || !foundEditedBBR || !foundRestoredSwap {
		t.Fatalf("post-reload invariants broken: custom=%v editedBBR=%v restoredSwap=%v", foundCustom, foundEditedBBR, foundRestoredSwap)
	}

	// 5. Calling LoadPresetTemplates again adds 0 and skips all 5
	res2, err := s.LoadPresetTemplates()
	if err != nil || res2.Added != 0 || res2.Skipped != 5 {
		t.Fatalf("second reload should add 0, skip 5; got %+v err=%v", res2, err)
	}

	// 6. Deleting all templates and reopening stays empty (does NOT auto-reseed v4 vault)
	for _, tpl := range s.ListTemplates() {
		if err := s.DeleteTemplate(tpl.ID); err != nil {
			t.Fatalf("delete %s: %v", tpl.ID, err)
		}
	}
	s2, err := Open(vaultFile, nil)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if len(s2.ListTemplates()) != 0 {
		t.Fatalf("expected empty templates after reopen, got %d", len(s2.ListTemplates()))
	}
}
