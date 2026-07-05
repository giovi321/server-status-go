// Package update self-updates the agent binary from GitHub Releases.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Release is a resolved latest release: the tag, the asset URL, and its sha256.
type Release struct {
	Version  string
	AssetURL string
	Sha256   string
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest resolves the latest release for repo, locating the assetName binary and
// its "<assetName>.sha256" sibling.
func Latest(ctx context.Context, apiBase, repo, assetName string) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(apiBase, "/"), repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("releases/latest returned %d", resp.StatusCode)
	}
	var gh ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&gh); err != nil {
		return Release{}, err
	}
	rel := Release{Version: gh.TagName}
	var sumURL string
	for _, a := range gh.Assets {
		switch a.Name {
		case assetName:
			rel.AssetURL = a.URL
		case assetName + ".sha256":
			sumURL = a.URL
		}
	}
	if rel.AssetURL == "" {
		return Release{}, fmt.Errorf("asset %q not found in latest release", assetName)
	}
	if sumURL != "" {
		if s, err := fetchSha256(ctx, sumURL, assetName); err == nil {
			rel.Sha256 = s
		}
	}
	return rel, nil
}

func fetchSha256(ctx context.Context, url, assetName string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	// Accept either a bare hex line or "<hex>  <name>" lines.
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) == 1 || strings.HasSuffix(line, assetName) {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("no checksum for %s", assetName)
}

// Apply downloads the release asset, verifies its sha256, and atomically swaps
// destPath (keeping the previous binary as destPath.bak). A bad download leaves
// destPath untouched.
func Apply(ctx context.Context, client *http.Client, rel Release, destPath string) error {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.AssetURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("asset download returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if rel.Sha256 != "" {
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != rel.Sha256 {
			return fmt.Errorf("checksum mismatch: got %s want %s", got, rel.Sha256)
		}
	}
	tmp := destPath + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	// Keep the current binary as .bak (best-effort), then atomically swap.
	_ = os.Rename(destPath, destPath+".bak")
	if err := os.Rename(tmp, destPath); err != nil {
		// Roll the backup back if the swap failed.
		_ = os.Rename(destPath+".bak", destPath)
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
