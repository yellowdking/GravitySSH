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

func TestIsTrustedGitHubURLStrict(t *testing.T) {
	valid := []string{
		"https://github.com/yellowdking/GravitySSH/releases/tag/v1.4.1",
		"https://github.com/yellowdking/GravitySSH/releases/download/v1.4.1/GravitySSH-v1.4.1-windows-amd64.exe",
		"https://GITHUB.COM/yellowdking/GravitySSH/releases/latest",
	}
	for _, u := range valid {
		if !isTrustedGitHubURL(u) {
			t.Errorf("expected trusted URL %q to be accepted", u)
		}
	}

	invalid := []string{
		"",
		"http://github.com/yellowdking/GravitySSH/releases/tag/v1.4.1",
		"https://github.com/attacker/GravitySSH/releases/download/v1.4.1/evil.exe",
		"https://github.com/yellowdking/OtherRepo/releases/tag/v1.0.0",
		"https://objects.githubusercontent.com/yellowdking/GravitySSH/releases/download/v1.4.1/app.exe",
		"https://user:pass@github.com/yellowdking/GravitySSH/releases/tag/v1.4.1",
		"https://github.com:443/yellowdking/GravitySSH/releases/tag/v1.4.1",
		"https://github.com:/yellowdking/GravitySSH/releases/tag/v1.4.1",
		"https://github.com/yellowdking/GravitySSH/releases/../../attacker/repo/releases/download/v1/evil.exe",
		"https://github.com/yellowdking/GravitySSH/releases/..%2f..%2fattacker/repo",
		"https://github.com/yellowdking/GravitySSH/releases/",
	}
	for _, u := range invalid {
		if isTrustedGitHubURL(u) {
			t.Errorf("expected untrusted URL %q to be rejected", u)
		}
	}
}

func TestCheckReleaseRedirectAndEmptyFieldGuards(t *testing.T) {
	// Verify CheckRedirect policy directly.
	okReq, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repositories/12345/releases/latest", nil)
	if err := checkReleaseRedirect(okReq, []*http.Request{okReq}); err != nil {
		t.Fatalf("expected https://api.github.com redirect to be allowed, got %v", err)
	}
	for _, badURL := range []string{
		"http://api.github.com/repos/yellowdking/GravitySSH/releases/latest",
		"https://evil.example.com/releases/latest",
		"https://github.com/yellowdking/GravitySSH/releases/latest",
		"https://user@api.github.com/repos/yellowdking/GravitySSH/releases/latest",
		"https://api.github.com:8443/repos/yellowdking/GravitySSH/releases/latest",
	} {
		req, _ := http.NewRequest(http.MethodGet, badURL, nil)
		if err := checkReleaseRedirect(req, []*http.Request{okReq}); err == nil {
			t.Errorf("expected redirect to %q to be rejected", badURL)
		}
	}
	if err := checkReleaseRedirect(okReq, make([]*http.Request, 5)); err == nil {
		t.Fatal("expected redirect loop (>=5) to be rejected")
	}

	// End-to-end redirect to non-api.github.com must fail without touching target server.
	var targetHits int
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits++
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9","html_url":"https://github.com/yellowdking/GravitySSH/releases/tag/v9.9.9"}`))
	}))
	defer targetSrv.Close()

	redirSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetSrv.URL, http.StatusFound)
	}))
	defer redirSrv.Close()

	if _, err := checkReleaseAtURL(context.Background(), redirSrv.URL, "1.4.0"); err == nil {
		t.Fatal("expected redirect to non-api.github.com host to fail")
	}
	if targetHits != 0 {
		t.Fatalf("redirect target must never be contacted, got %d hits", targetHits)
	}

	// Empty html_url or tag_name must be rejected.
	for _, payload := range []string{
		`{"tag_name": "v1.4.1", "html_url": ""}`,
		`{"tag_name": "", "html_url": "https://github.com/yellowdking/GravitySSH/releases/tag/v1.4.1"}`,
		`{"tag_name": "v1.4.1", "html_url": "https://github.com/attacker/GravitySSH/releases/tag/v1.4.1"}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(payload))
		}))
		_, err := checkReleaseAtURL(context.Background(), srv.URL, "1.4.0")
		srv.Close()
		if err == nil {
			t.Fatalf("expected error for invalid payload %s", payload)
		}
	}
}
