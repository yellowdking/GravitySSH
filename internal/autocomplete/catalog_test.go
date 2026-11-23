package autocomplete

import (
	"testing"

	"github.com/yellowdking/GravitySSH/internal/models"
)

func TestSuggestBuiltinsAndInsertSuffix(t *testing.T) {
	items := Suggest("apt ", nil, nil, false, false, 8)
	if len(items) == 0 {
		t.Fatal("expected suggestions for 'apt '")
	}
	foundUpdate := false
	for _, it := range items {
		if it.Command == "apt update" {
			foundUpdate = true
			if it.InsertText != "update" {
				t.Fatalf("expected InsertText 'update', got %q", it.InsertText)
			}
		}
	}
	if !foundUpdate {
		t.Fatalf("expected 'apt update' in suggestions: %+v", items)
	}

	dockItems := Suggest("dock", nil, nil, false, false, 8)
	if len(dockItems) == 0 {
		t.Fatal("expected suggestions for 'dock'")
	}
	if dockItems[0].InsertText[:2] != "er" {
		t.Fatalf("expected InsertText starting with 'er', got %q", dockItems[0].InsertText)
	}

	apkItems := Suggest("apk a", nil, nil, false, false, 5)
	if len(apkItems) == 0 || apkItems[0].Command != "apk add --no-cache " {
		t.Fatalf("expected 'apk add --no-cache ' for 'apk a', got %+v", apkItems)
	}
}

func TestSuggestPrioritizesShortcutsAndHistory(t *testing.T) {
	shortcuts := []models.CommandShortcut{
		{Name: "部署后端", Command: "docker compose up -d --build", Category: "部署", Pinned: true},
	}
	history := []models.CommandRecord{
		{Command: "docker logs -f api-gateway", ServerName: "Prod-1"},
	}

	items := Suggest("docker ", shortcuts, history, true, true, 10)
	if len(items) == 0 {
		t.Fatal("expected suggestions")
	}
	if items[0].Source != "shortcut" {
		t.Fatalf("expected pinned shortcut to rank first, got %+v", items[0])
	}

	noHist := Suggest("docker logs -f api", shortcuts, history, true, false, 10)
	for _, it := range noHist {
		if it.Source == "history" {
			t.Fatal("expected no history items when includeHistory=false")
		}
	}
}
