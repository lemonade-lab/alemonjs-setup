// Package releases reads public GitHub release metadata for supported AlemonJS apps.
package releases

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
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

var allowed = map[string]string{"alemondesk": "lemonade-lab/alemondesk", "alemonapp": "lemonade-lab/alemonapp", "albs": "lemonade-lab/albs", "alemonjs-setup": "lemonade-lab/alemonjs-setup"}

type Update struct {
	Current         string `json:"current"`
	Latest          string `json:"latest,omitempty"`
	Available       bool   `json:"available"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
	DownloadURL     string `json:"downloadUrl,omitempty"`
	AssetName       string `json:"assetName,omitempty"`
	PlatformMatched bool   `json:"platformMatched"`
	DownloadReady   bool   `json:"downloadReady"`
}

func SetupUpdate(current string) (Update, error) {
	result := Update{Current: current}
	items, err := List("alemonjs-setup")
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
