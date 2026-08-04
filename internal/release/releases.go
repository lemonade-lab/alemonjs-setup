// Package release reads public GitHub release metadata for supported AlemonJS apps.
package release

import (
	"encoding/json"
	"fmt"
	"net/http"
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

var allowed = map[string]string{"alemondesk": "lemonade-lab/alemondesk", "alemonapp": "lemonade-lab/alemonapp", "alemongo": "lemonade-lab/alemongo"}

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
