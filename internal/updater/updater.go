package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

const (
	latestReleaseAPI         = "https://api.github.com/repos/yellowdking/GravitySSH/releases/latest"
	trustedReleasePathPrefix = "/yellowdking/GravitySSH/releases/"
	maxReleaseResponseSize   = 4 << 20 // 4 MiB
)

func checkReleaseRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("更新接口重定向次数过多")
	}
	if req == nil || req.URL == nil || !strings.EqualFold(req.URL.Scheme, "https") {
		return errors.New("拒绝非 HTTPS 更新重定向")
	}
	if req.URL.User != nil || req.URL.Port() != "" || !strings.EqualFold(req.URL.Host, "api.github.com") {
		return errors.New("拒绝跳转到不受信任的更新主机")
	}
	return nil
}

var releaseHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 10 * time.Second,
	},
	CheckRedirect: checkReleaseRedirect,
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	PublishedAt string    `json:"published_at"`
	Assets      []ghAsset `json:"assets"`
}

// CheckLatestRelease queries GitHub Releases API to see if a newer GravitySSH version exists.
func CheckLatestRelease(ctx context.Context, currentVersion string) (models.UpdateCheckResult, error) {
	return checkReleaseAtURL(ctx, latestReleaseAPI, currentVersion)
}

func isTrustedGitHubURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.Contains(trimmed, "\\") {
		return false
	}
	u, err := url.Parse(trimmed)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Opaque != "" {
		return false
	}
	if u.User != nil || u.Port() != "" || !strings.EqualFold(u.Host, "github.com") {
		return false
	}
	lowerRawPath := strings.ToLower(u.RawPath)
	if strings.Contains(lowerRawPath, "%2f") || strings.Contains(lowerRawPath, "%5c") || strings.Contains(lowerRawPath, "%2e") {
		return false
	}
	if !strings.HasPrefix(u.Path, trustedReleasePathPrefix) || len(u.Path) <= len(trustedReleasePathPrefix) {
		return false
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
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
	if !isTrustedGitHubURL(releaseURL) {
		return models.UpdateCheckResult{}, errors.New("更新接口返回了不受信任的发布页地址")
	}

	latest := stripVersionPrefix(rel.TagName)
	if _, ok := parseSemver(latest); !ok {
		return models.UpdateCheckResult{}, errors.New("更新接口返回了无效的版本号")
	}

	downloadURL := selectReleaseAssetURL(runtime.GOOS, runtime.GOARCH, rel.Assets, releaseURL)

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
	l, lok := parseSemver(latest)
	if !lok {
		return false
	}
	c, cok := parseSemver(current)
	if !cok {
		return false
	}
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
		cn, cnum := parseASCIIDigits(c.pre[i])
		ln, lnum := parseASCIIDigits(l.pre[i])
		switch {
		case cnum && lnum:
			return ln > cn
		case cnum && !lnum:
			return true // numeric < alphanumeric
		case !cnum && lnum:
			return false
		default:
			return l.pre[i] > c.pre[i]
		}
	}
	return len(l.pre) > len(c.pre)
}

func parseASCIIDigits(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func parseSemver(v string) (semver, bool) {
	v = stripVersionPrefix(v)
	if v == "" {
		return semver{}, false
	}
	if idx := strings.IndexByte(v, '+'); idx >= 0 {
		v = v[:idx]
	}
	core, preStr, hasPre := strings.Cut(v, "-")
	if core == "" {
		return semver{}, false
	}
	parts := strings.Split(core, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return semver{}, false
	}
	var out semver
	for i := 0; i < len(parts); i++ {
		n, ok := parseASCIIDigits(parts[i])
		if !ok || n > uint64(^uint(0)>>1) {
			return semver{}, false
		}
		out.nums[i] = int(n)
	}
	if hasPre {
		if preStr == "" {
			return semver{}, false
		}
		preParts := strings.Split(preStr, ".")
		for _, p := range preParts {
			if p == "" {
				return semver{}, false
			}
		}
		out.pre = preParts
	}
	return out, true
}

func archMatchesAsset(goarch, lowerName string) bool {
	switch goarch {
	case "arm64":
		return strings.Contains(lowerName, "arm64") || strings.Contains(lowerName, "aarch64") || strings.Contains(lowerName, "universal")
	case "amd64":
		return strings.Contains(lowerName, "amd64") || strings.Contains(lowerName, "x86_64") || strings.Contains(lowerName, "x64") || strings.Contains(lowerName, "universal")
	default:
		return strings.Contains(lowerName, goarch)
	}
}

func selectReleaseAssetURL(goos, goarch string, assets []ghAsset, fallbackURL string) string {
	var osFallback string
	for _, asset := range assets {
		dl := strings.TrimSpace(asset.BrowserDownloadURL)
		if !isTrustedGitHubURL(dl) {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(asset.Name))
		switch goos {
		case "windows":
			if !strings.HasSuffix(name, ".exe") {
				continue
			}
			if archMatchesAsset(goarch, name) {
				return dl
			}
			if osFallback == "" {
				osFallback = dl
			}
		case "darwin":
			isMacAsset := strings.HasSuffix(name, ".dmg") || strings.HasSuffix(name, ".pkg") ||
				((strings.Contains(name, "darwin") || strings.Contains(name, "macos")) && (strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tar.gz")))
			if !isMacAsset {
				continue
			}
			if archMatchesAsset(goarch, name) {
				return dl
			}
			if osFallback == "" {
				osFallback = dl
			}
		case "linux":
			isLinuxAsset := strings.HasSuffix(name, ".appimage") || strings.HasSuffix(name, ".deb") || strings.HasSuffix(name, ".rpm") ||
				(strings.Contains(name, "linux") && (strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz") || strings.HasSuffix(name, ".zip")))
			if !isLinuxAsset {
				continue
			}
			if archMatchesAsset(goarch, name) {
				return dl
			}
			if osFallback == "" {
				osFallback = dl
			}
		}
	}
	if osFallback != "" {
		return osFallback
	}
	return fallbackURL
}
