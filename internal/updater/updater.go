package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

const (
	latestReleaseAPI       = "https://api.github.com/repos/yellowdking/GravitySSH/releases/latest"
	maxReleaseResponseSize = 4 << 20 // 4 MiB
)

var releaseHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 10 * time.Second,
	},
}

type ghRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// CheckLatestRelease queries GitHub Releases API to see if a newer GravitySSH version exists.
func CheckLatestRelease(ctx context.Context, currentVersion string) (models.UpdateCheckResult, error) {
	return checkReleaseAtURL(ctx, latestReleaseAPI, currentVersion)
}

func isTrustedGitHubURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "github.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

func stripVersionPrefix(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 0 && (v[0] == 'v' || v[0] == 'V') {
		return v[1:]
	}
	return v
}

func checkReleaseAtURL(ctx context.Context, apiURL, currentVersion string) (models.UpdateCheckResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cur := stripVersionPrefix(currentVersion)

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, apiURL, nil)
	if err != nil {
		return models.UpdateCheckResult{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "GravitySSH/"+cur)

	resp, err := releaseHTTPClient.Do(req)
	if err != nil {
		return models.UpdateCheckResult{}, fmt.Errorf("检查更新失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return models.UpdateCheckResult{}, fmt.Errorf("GitHub API 返回状态 %d", resp.StatusCode)
	}

	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseResponseSize)).Decode(&rel); err != nil {
		return models.UpdateCheckResult{}, fmt.Errorf("解析版本信息失败: %w", err)
	}

	releaseURL := strings.TrimSpace(rel.HTMLURL)
	if releaseURL != "" && !isTrustedGitHubURL(releaseURL) {
		return models.UpdateCheckResult{}, errors.New("更新接口返回了不受信任的发布页地址")
	}

	downloadURL := releaseURL
	for _, asset := range rel.Assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ".exe") && isTrustedGitHubURL(asset.BrowserDownloadURL) {
			downloadURL = strings.TrimSpace(asset.BrowserDownloadURL)
			break
		}
	}

	latest := stripVersionPrefix(rel.TagName)

	return models.UpdateCheckResult{
		CurrentVersion: cur,
		LatestVersion:  latest,
		HasUpdate:      IsNewerVersion(cur, latest),
		ReleaseURL:     releaseURL,
		DownloadURL:    downloadURL,
		ReleaseNotes:   strings.TrimSpace(rel.Body),
		PublishedAt:    rel.PublishedAt,
	}, nil
}

type semver struct {
	nums [3]int
	pre  []string
}

// IsNewerVersion compares two semantic versions (e.g. "1.1.3" vs "1.2.0", "1.2.0-beta" vs "1.2.0").
func IsNewerVersion(current, latest string) bool {
	c := parseSemver(current)
	l := parseSemver(latest)
	for i := 0; i < 3; i++ {
		if l.nums[i] != c.nums[i] {
			return l.nums[i] > c.nums[i]
		}
	}
	// Per SemVer 2.0.0: a version with pre-release identifiers has lower precedence than a normal version.
	if len(c.pre) == 0 && len(l.pre) == 0 {
		return false
	}
	if len(c.pre) > 0 && len(l.pre) == 0 {
		return true
	}
	if len(c.pre) == 0 && len(l.pre) > 0 {
		return false
	}
	for i := 0; i < len(c.pre) && i < len(l.pre); i++ {
		if c.pre[i] == l.pre[i] {
			continue
		}
		cn, cerr := strconv.Atoi(c.pre[i])
		ln, lerr := strconv.Atoi(l.pre[i])
		switch {
		case cerr == nil && lerr == nil:
			return ln > cn
		case cerr == nil && lerr != nil:
			return true // numeric < alphanumeric
		case cerr != nil && lerr == nil:
			return false
		default:
			return l.pre[i] > c.pre[i]
		}
	}
	return len(l.pre) > len(c.pre)
}

func parseSemver(v string) semver {
	v = stripVersionPrefix(v)
	if idx := strings.IndexByte(v, '+'); idx >= 0 {
		v = v[:idx]
	}
	core, preStr, hasPre := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	var out semver
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err == nil && n >= 0 {
			out.nums[i] = n
		}
	}
	if hasPre && preStr != "" {
		out.pre = strings.Split(preStr, ".")
	}
	return out
}
