package vault

import (
	"strings"
	"testing"
)

func TestBuiltinOpsTemplatesCompleteAndSafe(t *testing.T) {
	tpls := BuiltinOpsTemplates()
	if len(tpls) != 5 {
		t.Fatalf("expected 5 curated VPS templates, got %d", len(tpls))
	}
	wantIDs := map[string]bool{
		"tpl-bbr":      false,
		"tpl-swap":     false,
		"tpl-mirror":   false,
		"tpl-docker":   false,
		"tpl-ssh-port": false,
	}
	for _, tpl := range tpls {
		if _, ok := wantIDs[tpl.ID]; !ok {
			t.Fatalf("unexpected template ID: %s", tpl.ID)
		}
		wantIDs[tpl.ID] = true
		if strings.TrimSpace(tpl.Name) == "" || strings.TrimSpace(tpl.Command) == "" {
			t.Fatalf("template %s has empty name or command", tpl.ID)
		}
		if tpl.ID == "tpl-ssh-port" {
			if !strings.Contains(tpl.Command, "sshd -t") || !strings.Contains(tpl.Command, "600") {
				t.Fatal("ssh-port template must validate sshd -t and include rollback safeguard")
			}
		}
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Fatalf("missing template %s", id)
		}
	}
}
