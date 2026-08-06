// Package releases reads public GitHub release metadata for supported AlemonJS apps.
package releases

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

type Item struct {
	Tag         string  `json:"tag"`
	Name        string  `json:"name"`
	URL         string  `json:"url"`
	PublishedAt string  `json:"publishedAt"`
	Assets      []Asset `json:"assets"`
}
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

var allowed = map[string]string{"alemondesk": "lemonade-lab/alemondesk", "alemonapp": "lemonade-lab/alemonapp", "alx": "lemonade-lab/alx", "alemonx": "lemonade-lab/alemonx"}

type Update struct {
	Current         string `json:"current"`
	Latest          string `json:"latest,omitempty"`
	Available       bool   `json:"available"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
	DownloadURL     string `json:"downloadUrl,omitempty"`
	AssetName       string `json:"assetName,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
	IntegrityReady  bool   `json:"integrityReady"`
	PlatformMatched bool   `json:"platformMatched"`
	DownloadReady   bool   `json:"downloadReady"`
}

func SetupUpdate(current string) (Update, error) {
	result := Update{Current: current}
	items, err := List("alemonx")
	if err != nil {
		return result, err
	}
	latest := items[0]
	result.Latest, result.ReleaseURL = latest.Tag, latest.URL
	result.Available = versionCompare(latest.Tag, current) > 0
	if !result.Available {
		return result, nil
	}
	asset := matchingAsset(latest.Assets)
	if asset.Name != "" {
		result.DownloadURL, result.AssetName, result.PlatformMatched = asset.URL, asset.Name, true
		result.SHA256, _ = checksumForAsset(latest.Assets, asset.Name)
		result.IntegrityReady = result.SHA256 != ""
	}
	return result, nil
}

func matchingAsset(assets []Asset) Asset {
	return matchingAssetFor(assets, runtime.GOOS, runtime.GOARCH)
}

// matchingAssetFor compares filename segments instead of substrings. In
// particular, "darwin" contains "win", so a strings.Contains(name, "win")
// check can incorrectly offer a macOS archive to a Windows user.
func matchingAssetFor(assets []Asset, platform, architecture string) Asset {
	for _, asset := range assets {
		tokens := assetNameTokens(asset.Name)
		system := (platform == "darwin" && (tokens["darwin"] || tokens["macos"] || tokens["mac"])) ||
			(platform == "windows" && (tokens["windows"] || tokens["win32"])) ||
			(platform == "linux" && tokens["linux"])
		arch := (architecture == "arm64" && (tokens["arm64"] || tokens["aarch64"])) ||
			(architecture == "amd64" && (tokens["amd64"] || tokens["x64"] || tokens["x86_64"]))
		if system && arch {
			return asset
		}
	}
	return Asset{}
}

func assetNameTokens(name string) map[string]bool {
	tokens := map[string]bool{}
	for _, token := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_')
	}) {
		tokens[token] = true
	}
	return tokens
}

func versionCompare(left, right string) int {
	left, right = normalizeVersion(left), normalizeVersion(right)
	if semver.IsValid(left) && semver.IsValid(right) {
		return semver.Compare(left, right)
	}
	parse := func(value string) [3]int {
		var result [3]int
		for index, part := range strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), ".") {
			if index >= 3 {
				break
			}
			number, err := strconv.Atoi(part)
			if err != nil {
				return [3]int{}
			}
			result[index] = number
		}
		return result
	}
	l, r := parse(left), parse(right)
	for index := range l {
		if l[index] > r[index] {
			return 1
		}
		if l[index] < r[index] {
			return -1
		}
	}
	return 0
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	return value
}

func checksumForAsset(assets []Asset, name string) (string, error) {
	for _, asset := range assets {
		upper := strings.ToUpper(asset.Name)
		if upper != "SHA256SUMS" && upper != "SHA256SUMS.TXT" && upper != "CHECKSUMS.TXT" {
			continue
		}
		response, err := (&http.Client{Timeout: 8 * time.Second}).Get(asset.URL)
		if err != nil {
			return "", err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK {
			return "", fmt.Errorf("无法读取发布校验文件")
		}
		for _, line := range strings.Split(string(body), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && strings.EqualFold(strings.TrimPrefix(fields[1], "*"), name) && len(fields[0]) == 64 {
				return strings.ToLower(fields[0]), nil
			}
		}
	}
	return "", nil
}

func List(id string) ([]Item, error) {
	repository, ok := allowed[id]
	if !ok {
		return nil, fmt.Errorf("不支持该下载项目")
	}
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Get("https://api.github.com/repos/" + repository + "/releases?per_page=30")
	if err != nil {
		return nil, fmt.Errorf("无法获取版本列表，请检查网络后重试")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub 暂时无法提供版本列表")
	}
	var data []struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		HTMLURL     string    `json:"html_url"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
		Assets      []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("版本列表内容无法识别")
	}
	items := make([]Item, 0, len(data))
	for _, item := range data {
		if item.Draft || item.Prerelease {
			continue
		}
		name := item.Name
		if name == "" {
			name = item.TagName
		}
		assets := make([]Asset, 0, len(item.Assets))
		for _, asset := range item.Assets {
			assets = append(assets, Asset{Name: asset.Name, URL: asset.BrowserDownloadURL, Size: asset.Size})
		}
		items = append(items, Item{Tag: item.TagName, Name: name, URL: item.HTMLURL, PublishedAt: item.PublishedAt.Format(time.RFC3339), Assets: assets})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("暂未找到可用的正式版本")
	}
	return items, nil
}
