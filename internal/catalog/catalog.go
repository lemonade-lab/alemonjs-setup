// Package catalog retrieves and formats the official AlemonJS ecosystem catalog.
package catalog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
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

type Document struct {
	Source   string `json:"source"`
	Markdown string `json:"markdown"`
}

type PackageConfigField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type PackageConfig struct {
	Package   string               `json:"package"`
	Namespace string               `json:"namespace"`
	Fields    []PackageConfigField `json:"fields"`
	Values    map[string]string    `json:"values"`
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
			} else if repository := repositoryURL(item.URL); repository != "" {
				item.Install = "git+" + repository + ".git"
			}
		}
	}
	return groups, nil
}

func repositoryURL(source string) string {
	parsed, err := url.Parse(source)
	if err != nil || (parsed.Host != "github.com" && parsed.Host != "gitee.com") {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return "https://" + parsed.Host + "/" + parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
}

// Document loads an online README only from the repository hosts represented
// by the official catalog. This keeps the local API from becoming a general
// network proxy while still allowing catalog entries to render their docs.
func LoadDocument(source string) (Document, error) {
	data, candidate, err := loadRepositoryFile(source, "README.md")
	if err != nil {
		return Document{}, err
	}
	return Document{Source: candidate, Markdown: string(data)}, nil
}

func LoadPackageConfig(source string) (PackageConfig, error) {
	data, _, err := loadRepositoryFile(source, "package.json")
	if err != nil {
		return PackageConfig{}, err
	}
	var manifest struct {
		Name     string `json:"name"`
		Alemonjs struct {
			Config  []PackageConfigField `json:"config"`
			Desktop struct {
				Platform []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"platform"`
			} `json:"desktop"`
		} `json:"alemonjs"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return PackageConfig{}, fmt.Errorf("在线 package.json 无法识别")
	}
	if manifest.Name == "" || len(manifest.Alemonjs.Config) == 0 {
		return PackageConfig{}, fmt.Errorf("该包没有声明 alemonjs.config")
	}
	namespace := manifest.Name
	for _, platform := range manifest.Alemonjs.Desktop.Platform {
		if platform.Value == manifest.Name && platform.Name != "" {
			namespace = platform.Name
			break
		}
	}
	return PackageConfig{Package: manifest.Name, Namespace: namespace, Fields: manifest.Alemonjs.Config, Values: map[string]string{}}, nil
}

func loadRepositoryFile(source, filename string) ([]byte, string, error) {
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme != "https" {
		return nil, "", fmt.Errorf("文档地址无效")
	}
	candidates, err := repositoryFileCandidates(parsed, filename)
	if err != nil {
		return nil, "", err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	for _, candidate := range candidates {
		response, err := client.Get(candidate)
		if err != nil || response.StatusCode != http.StatusOK {
			if response != nil {
				response.Body.Close()
			}
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20))
		response.Body.Close()
		if readErr != nil {
			return nil, "", fmt.Errorf("读取在线文档失败")
		}
		if len(data) == 2<<20 {
			return nil, "", fmt.Errorf("在线文档过大，无法显示")
		}
		return data, candidate, nil
	}
	return nil, "", fmt.Errorf("暂时无法读取在线文档")
}

// repositoryFileCandidates keeps a catalog URL's file or directory context.
// A blob/raw Markdown URL is read as-is; a tree/directory URL resolves files
// in that directory, so packages can live below a repository root.
func repositoryFileCandidates(parsed *url.URL, filename string) ([]string, error) {
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	isDocument := filename == "README.md"
	switch parsed.Host {
	case "raw.githubusercontent.com":
		if isDocument {
			return []string{parsed.String()}, nil
		}
		if len(parts) < 4 {
			return nil, fmt.Errorf("仓库地址无效")
		}
		return []string{"https://raw.githubusercontent.com/" + path.Join(append(parts[:len(parts)-1], filename)...)}, nil
	case "github.com":
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("仓库地址无效")
		}
		repo := path.Join(parts[0], strings.TrimSuffix(parts[1], ".git"))
		return githubCandidates(repo, parts[2:], filename, isDocument), nil
	case "gitee.com":
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("仓库地址无效")
		}
		repo := path.Join(parts[0], strings.TrimSuffix(parts[1], ".git"))
		return giteeCandidates(repo, parts[2:], filename, isDocument), nil
	default:
		return nil, fmt.Errorf("暂不支持该文档来源")
	}
}

func githubCandidates(repo string, suffix []string, filename string, isDocument bool) []string {
	branches, directory, direct := sourceLocation(suffix, isDocument)
	result := make([]string, 0, len(branches))
	for _, branch := range branches {
		target := append([]string{repo, branch}, directory...)
		if !direct {
			target = append(target, filename)
		}
		result = append(result, "https://raw.githubusercontent.com/"+path.Join(target...))
	}
	return result
}

func giteeCandidates(repo string, suffix []string, filename string, isDocument bool) []string {
	branches, directory, direct := sourceLocation(suffix, isDocument)
	result := make([]string, 0, len(branches))
	for _, branch := range branches {
		target := append([]string{repo, "raw", branch}, directory...)
		if !direct {
			target = append(target, filename)
		}
		result = append(result, "https://gitee.com/"+path.Join(target...))
	}
	return result
}

func sourceLocation(suffix []string, isDocument bool) (branches, directory []string, direct bool) {
	if len(suffix) >= 2 && (suffix[0] == "blob" || suffix[0] == "tree") {
		branches, directory = []string{suffix[1]}, suffix[2:]
		direct = suffix[0] == "blob" && isDocument
		if !isDocument && suffix[0] == "blob" && len(directory) > 0 && strings.Contains(path.Base(directory[len(directory)-1]), ".") {
			directory = directory[:len(directory)-1]
		}
		return
	}
	if isDocument && len(suffix) > 0 && strings.HasSuffix(strings.ToLower(suffix[len(suffix)-1]), ".md") {
		return []string{"main", "master"}, suffix, true
	}
	if !isDocument && len(suffix) > 0 && strings.Contains(path.Base(suffix[len(suffix)-1]), ".") {
		suffix = suffix[:len(suffix)-1]
	}
	return []string{"main", "master"}, suffix, false
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
