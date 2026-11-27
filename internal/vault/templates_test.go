package vault

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yellowdking/GravitySSH/internal/models"
)

func TestBuiltinOpsTemplatesCompleteAndSafe(t *testing.T) {
	tpls := BuiltinOpsTemplates()
	if len(tpls) != 6 {
		t.Fatalf("expected 6 curated VPS templates, got %d", len(tpls))
	}
	wantIDs := map[string]bool{
		"tpl-bbr": false, "tpl-swap": false, "tpl-mirror": false, "tpl-docker": false, "tpl-ssh-port": false, "tpl-inspect": false,
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
		if tpl.ID == "tpl-ssh-port" {
			if !strings.Contains(tpl.Command, "sshd -t") || !strings.Contains(tpl.Command, "600") {
				t.Fatal("ssh-port template must validate sshd -t and include rollback safeguard")
			}
			if strings.Contains(tpl.Command, "/tmp/.gssh_port_pending") {
				t.Fatal("ssh-port template must not use predictable world-writable /tmp marker")
			}
			markerIdx := strings.Index(tpl.Command, "/run/gssh_port_pending")
			nohupIdx := strings.Index(tpl.Command, "sudo nohup sh -c")
			if markerIdx < 0 || nohupIdx < 0 || markerIdx > nohupIdx {
				t.Fatalf("ssh-port template must create root-owned /run/gssh_port_pending before launching sudo nohup rollback (marker=%d, nohup=%d)", markerIdx, nohupIdx)
			}
			if !strings.Contains(tpl.Command, "sudo rm -f /run/gssh_port_pending") {
				t.Fatal("ssh-port template must tell user to cancel rollback with sudo rm -f /run/gssh_port_pending")
			}
			afterNohup := tpl.Command[nohupIdx:]
			lineEnd := strings.IndexByte(afterNohup, '\n')
			if lineEnd > 0 {
				afterNohup = afterNohup[:lineEnd]
			}
			if strings.Contains(afterNohup[len("sudo nohup sh -c"):], "sudo ") {
				t.Fatalf("rollback command inside sudo nohup sh -c must not invoke sudo after sleep 600: %s", afterNohup)
			}
		}
		if tpl.ID == "tpl-swap" && strings.Contains(tpl.Command, "exit 0") {
			t.Fatal("tpl-swap should avoid exit 0 so it can never terminate a login shell")
		}
		if strings.Contains(tpl.Command, "exit 0") || strings.Contains(tpl.Command, "exit 1") {
			trimmed := strings.TrimSpace(tpl.Command)
			if !strings.HasPrefix(trimmed, "(") || !strings.HasSuffix(trimmed, ")") {
				t.Fatalf("template %s uses exit without subshell wrapper", tpl.ID)
			}
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

	if got := len(s.ListTemplates()); got != 6 {
		t.Fatalf("expected 6 initial templates, got %d", got)
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

	// 4. LoadPresetTemplates: restores ONLY tpl-swap, skips the other 5 (including edited tpl-bbr), keeps custom
	res, err := s.LoadPresetTemplates()
	if err != nil {
		t.Fatalf("LoadPresetTemplates failed: %v", err)
	}
	if res.Added != 1 || res.Skipped != 5 || len(res.Templates) != 7 {
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

	// 5. Calling LoadPresetTemplates again adds 0 and skips all 6
	res2, err := s.LoadPresetTemplates()
	if err != nil || res2.Added != 0 || res2.Skipped != 6 {
		t.Fatalf("second reload should add 0, skip 6; got %+v err=%v", res2, err)
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
