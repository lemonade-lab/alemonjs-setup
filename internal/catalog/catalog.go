// Package catalog retrieves and formats the official AlemonJS ecosystem catalog.
package catalog

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Item struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Install     string `json:"install"`
}
type Group struct {
	Title string `json:"title"`
	Items []Item `json:"items"`
}

var sources = map[string]string{
	"apps":        "https://raw.githubusercontent.com/lemonade-lab/alemonjs.dev/main/docs/apps.md",
	"environment": "https://raw.githubusercontent.com/lemonade-lab/alemonjs.dev/main/docs/environment.md",
}

func Fetch(kind string) ([]Group, error) {
	url, ok := sources[kind]
	if !ok {
		return nil, fmt.Errorf("不支持的生态目录")
	}
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("无法读取官方目录，请检查网络后重试")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("官方目录暂时不可用")
	}
	var groups []Group
	references := map[string]string{}
	current := -1
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if name, link, ok := referenceDefinition(line); ok {
			references[name] = link
			continue
		}
		if strings.HasPrefix(line, "### ") {
			groups = append(groups, Group{Title: strings.TrimSpace(strings.TrimPrefix(line, "### "))})
			current = len(groups) - 1
			continue
		}
		if current < 0 || !strings.HasPrefix(line, "|") || strings.Contains(line, "---") || strings.Contains(line, "项目名") {
			continue
		}
		columns := strings.Split(line, "|")
		if len(columns) < 4 {
			continue
		}
		name, link := markdownLink(strings.TrimSpace(columns[1]))
		if name == "" {
			continue
		}
		groups[current].Items = append(groups[current].Items, Item{Name: name, URL: link, Description: strings.TrimSpace(columns[2])})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取官方目录失败")
	}
	for groupIndex := range groups {
		for itemIndex := range groups[groupIndex].Items {
			item := &groups[groupIndex].Items[itemIndex]
			if item.URL == "" {
				item.URL = references[item.Name]
			}
			if strings.HasPrefix(item.Name, "@alemonjs/") || item.Name == "alemonjs" {
				item.Install = item.Name
			} else if strings.HasPrefix(item.URL, "https://github.com/") || strings.HasPrefix(item.URL, "https://gitee.com/") {
				item.Install = "git+" + item.URL + ".git"
			}
		}
	}
	return groups, nil
}

func markdownLink(value string) (string, string) {
	if !strings.HasPrefix(value, "[") {
		return value, ""
	}
	end := strings.Index(value, "](")
	if end >= 0 && strings.HasSuffix(value, ")") {
		return value[1:end], value[end+2 : len(value)-1]
	}
	if strings.HasSuffix(value, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"), ""
	}
	return value, ""
}

func referenceDefinition(value string) (string, string, bool) {
	if !strings.HasPrefix(value, "[") {
		return "", "", false
	}
	end := strings.Index(value, "]:")
	if end < 1 {
		return "", "", false
	}
	name := value[1:end]
	link := strings.TrimSpace(value[end+2:])
	return name, link, name != "" && link != ""
}
