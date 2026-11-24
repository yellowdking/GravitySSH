package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		cur, latest string
		want        bool
	}{
		{"1.1.3", "1.2.0", true},
		{"v1.1.3", "v1.1.4", true},
		{"1.2.0", "1.2.0", false},
		{"1.2.1", "1.2.0", false},
		{"1.9.0", "1.10.0", true},
		{"1.2.0-beta", "1.2.1", true},
	}
	for _, tc := range cases {
		if got := IsNewerVersion(tc.cur, tc.latest); got != tc.want {
			t.Errorf("IsNewerVersion(%q, %q) = %v, want %v", tc.cur, tc.latest, got, tc.want)
		}
	}
}

func TestCheckReleaseAtURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v1.2.0",
			"html_url": "https://github.com/yellowdking/GravitySSH/releases/tag/v1.2.0",
			"body": "Release notes",
			"published_at": "2026-11-24T00:00:00Z",
			"assets": [
				{"name": "GravitySSH-v1.2.0-windows-amd64-portable.zip", "browser_download_url": "https://example.com/app.zip"},
				{"name": "GravitySSH-v1.2.0-windows-amd64.exe", "browser_download_url": "https://example.com/app.exe"}
			]
		}`))
	}))
	defer srv.Close()

	res, err := checkReleaseAtURL(context.Background(), srv.URL, "1.1.3")
	if err != nil {
		t.Fatalf("checkReleaseAtURL failed: %v", err)
	}
	if !res.HasUpdate || res.LatestVersion != "1.2.0" || res.DownloadURL != "https://example.com/app.exe" {
		t.Fatalf("unexpected update result: %+v", res)
	}
}
