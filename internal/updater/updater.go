package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

const latestReleaseAPI = "https://api.github.com/repos/yellowdking/GravitySSH/releases/latest"

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

func checkReleaseAtURL(ctx context.Context, apiURL, currentVersion string) (models.UpdateCheckResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, apiURL, nil)
	if err != nil {
		return models.UpdateCheckResult{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "GravitySSH/"+strings.TrimPrefix(strings.TrimSpace(currentVersion), "v"))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return models.UpdateCheckResult{}, fmt.Errorf("检查更新失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return models.UpdateCheckResult{}, fmt.Errorf("GitHub API 返回状态 %d", resp.StatusCode)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return models.UpdateCheckResult{}, fmt.Errorf("解析版本信息失败: %w", err)
	}

	downloadURL := rel.HTMLURL
	for _, asset := range rel.Assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ".exe") {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	cur := strings.TrimPrefix(strings.TrimSpace(currentVersion), "v")

	return models.UpdateCheckResult{
		CurrentVersion: cur,
		LatestVersion:  latest,
		HasUpdate:      IsNewerVersion(cur, latest),
		ReleaseURL:     rel.HTMLURL,
		DownloadURL:    downloadURL,
		ReleaseNotes:   strings.TrimSpace(rel.Body),
		PublishedAt:    rel.PublishedAt,
	}, nil
}

// IsNewerVersion compares two dotted semantic versions (e.g. "1.1.3" vs "1.2.0").
func IsNewerVersion(current, latest string) bool {
	cParts := parseSemver(current)
	lParts := parseSemver(latest)
	for i := 0; i < 3; i++ {
		if lParts[i] != cParts[i] {
			return lParts[i] > cParts[i]
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	var out [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err == nil && n >= 0 {
			out[i] = n
		}
	}
	return out
}
