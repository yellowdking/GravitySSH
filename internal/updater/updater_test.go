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
		{"V1.1.3", "V1.2.0", true},
		{"1.2.0", "1.2.0", false},
		{"1.2.1", "1.2.0", false},
		{"1.9.0", "1.10.0", true},
		{"1.2.0-beta", "1.2.1", true},
		{"1.2.0-beta", "1.2.0", true},
		{"1.2.0", "1.2.0-beta", false},
		{"1.2.0-alpha.2", "1.2.0-alpha.10", true},
		{"1.2.0-rc.1", "1.2.0-beta.2", false},
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
			"tag_name": "V1.2.0",
			"html_url": "https://github.com/yellowdking/GravitySSH/releases/tag/v1.2.0",
			"body": "Release notes",
			"published_at": "2026-11-24T00:00:00Z",
			"assets": [
				{"name": "GravitySSH-v1.2.0-windows-amd64-portable.zip", "browser_download_url": "https://github.com/yellowdking/GravitySSH/releases/download/v1.2.0/app.zip"},
				{"name": "GravitySSH-v1.2.0-windows-amd64.exe", "browser_download_url": "https://github.com/yellowdking/GravitySSH/releases/download/v1.2.0/app.exe"}
			]
		}`))
	}))
	defer srv.Close()

	res, err := checkReleaseAtURL(context.Background(), srv.URL, "1.1.3")
	if err != nil {
		t.Fatalf("checkReleaseAtURL failed: %v", err)
	}
	wantDL := "https://github.com/yellowdking/GravitySSH/releases/download/v1.2.0/app.exe"
	if !res.HasUpdate || res.LatestVersion != "1.2.0" || res.DownloadURL != wantDL {
		t.Fatalf("unexpected update result: %+v", res)
	}
}

func TestCheckReleaseRejectsUntrustedURLs(t *testing.T) {
	// Untrusted asset URL falls back to trusted GitHub HTMLURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v1.2.0",
			"html_url": "https://github.com/yellowdking/GravitySSH/releases/tag/v1.2.0",
			"assets": [
				{"name": "evil.exe", "browser_download_url": "https://evil.example.com/evil.exe"}
			]
		}`))
	}))
	defer srv.Close()

	res, err := checkReleaseAtURL(context.Background(), srv.URL, "1.1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.DownloadURL != res.ReleaseURL {
		t.Fatalf("expected fallback to ReleaseURL, got %q", res.DownloadURL)
	}

	// Untrusted HTMLURL is rejected outright
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name": "v1.2.0", "html_url": "http://evil.example.com/phish"}`))
	}))
	defer badSrv.Close()

	if _, err := checkReleaseAtURL(context.Background(), badSrv.URL, "1.1.0"); err == nil {
		t.Fatal("expected error for non-GitHub/non-HTTPS html_url")
	}
}
