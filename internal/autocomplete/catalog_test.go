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

func TestSuggestWithFilesDirectoriesAndScripts(t *testing.T) {
	entries := []models.SFTPFileEntry{
		{Name: "app", IsDir: true},
		{Name: "docker", IsDir: true},
		{Name: "nginx.conf", IsDir: false, Size: 2048},
		{Name: "backup.zip", IsDir: false, Size: 3 * 1024 * 1024},
		{Name: "script.sh", IsDir: false, Size: 512},
		{Name: ".env", IsDir: false, Size: 128},
	}

	// cd prioritizes directories only
	cdItems := SuggestWithFiles("cd d", "/srv", entries, nil, nil, false, false, 8)
	if len(cdItems) == 0 || cdItems[0].Command != "cd docker/" || cdItems[0].Source != "dir" || cdItems[0].InsertText != "ocker/" {
		t.Fatalf("unexpected cd suggestions: %+v", cdItems)
	}
	for _, it := range cdItems {
		if it.Source == "file" {
			t.Fatalf("cd should not suggest files, got %+v", it)
		}
	}

	// nano matches files and shows file size description
	nanoItems := SuggestWithFiles("nano ng", "/srv", entries, nil, nil, false, false, 8)
	if len(nanoItems) == 0 || nanoItems[0].Command != "nano nginx.conf" || nanoItems[0].Source != "file" || nanoItems[0].InsertText != "inx.conf" {
		t.Fatalf("unexpected nano suggestions: %+v", nanoItems)
	}

	// ./ prefix matches local scripts/dirs
	dotItems := SuggestWithFiles("./sc", "/srv", entries, nil, nil, false, false, 8)
	if len(dotItems) == 0 || dotItems[0].Command != "./script.sh" || dotItems[0].InsertText != "ript.sh" {
		t.Fatalf("unexpected ./ suggestions: %+v", dotItems)
	}

	// Hidden files only shown when prefix starts with '.'
	hiddenOff := SuggestWithFiles("cat ", "/srv", entries, nil, nil, false, false, 10)
	for _, it := range hiddenOff {
		if it.Command == "cat .env" {
			t.Fatal("dotfiles should be hidden unless prefix starts with '.'")
		}
	}
	hiddenOn := SuggestWithFiles("cat .", "/srv", entries, nil, nil, false, false, 10)
	if len(hiddenOn) == 0 || hiddenOn[0].Command != "cat .env" {
		t.Fatalf("expected .env when typing 'cat .', got %+v", hiddenOn)
	}

	// ExtractPathContext handles relative, absolute, and flag skips
	if dir, ok := ExtractPathContext("ls ./sub/fi", "/home/ubuntu"); !ok || dir != "/home/ubuntu/sub" {
		t.Fatalf("unexpected relative path context: (%q, %v)", dir, ok)
	}
	if dir, ok := ExtractPathContext("nano /etc/ng", "/home/ubuntu"); !ok || dir != "/etc" {
		t.Fatalf("unexpected abs path context: (%q, %v)", dir, ok)
	}
	if _, ok := ExtractPathContext("rm -rf", "/home/ubuntu"); ok {
		t.Fatal("expected flag token '-rf' not to trigger file path context")
	}
}
