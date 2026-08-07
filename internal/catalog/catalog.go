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
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/semver"
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

// PackageVersions is the small, UI-safe portion of the npm package document.
// It lets the install screen offer published versions without exposing the
// registry's full metadata payload to the browser.
type PackageVersions struct {
	Latest   string   `json:"latest"`
	Versions []string `json:"versions"`
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
	groups, references, err := parseCatalog(response.Body)
	if err != nil {
		return nil, err
	}
	for groupIndex := range groups {
		for itemIndex := range groups[groupIndex].Items {
			item := &groups[groupIndex].Items[itemIndex]
			if item.URL == "" {
				item.URL = references[item.Name]
			}
			if strings.HasPrefix(item.Name, "@alemonjs/") || item.Name == "alemonjs" {
				item.Install = item.Name
			} else if repository := repositoryInstallURL(item.URL); repository != "" {
				item.Install = "git+" + repository
			}
		}
	}
	return groups, nil
}

// parseCatalog keeps the meaning of a Markdown table instead of assuming that
// the second column is always its description. Connection tables, for example,
// use “项目 | 版本 | 说明”; the version badge is not user-facing copy.
//
// The description column is selected by its header (说明 / docs / description).
// Old two-column catalogs without such a header retain the former second-column
// fallback for backwards compatibility.
func parseCatalog(reader io.Reader) ([]Group, map[string]string, error) {
	var groups []Group
	references := map[string]string{}
	current := -1
	columns := catalogColumns{name: 0, description: -1}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if name, link, ok := referenceDefinition(line); ok {
			references[name] = link
			continue
		}
		if strings.HasPrefix(line, "### ") {
			groups = append(groups, Group{Title: strings.TrimSpace(strings.TrimPrefix(line, "### "))})
			current = len(groups) - 1
			columns = catalogColumns{name: 0, description: -1}
			continue
		}
		if current < 0 || !strings.HasPrefix(line, "|") || isMarkdownTableDivider(line) {
			continue
		}
		values := markdownTableValues(line)
		if len(values) < 2 {
			continue
		}
		if header, ok := parseCatalogTableHeader(values); ok {
			columns = header
			continue
		}
		nameIndex := columns.name
		if nameIndex < 0 || nameIndex >= len(values) {
			nameIndex = 0
		}
		name, link := markdownLink(values[nameIndex])
		if name == "" || isCatalogTableHeader(name) {
			continue
		}
		descriptionIndex := columns.description
		if descriptionIndex < 0 {
			descriptionIndex = 1
		}
		description := ""
		if descriptionIndex < len(values) {
			description = strings.TrimSpace(values[descriptionIndex])
		}
		groups[current].Items = append(groups[current].Items, Item{Name: name, URL: link, Description: description})
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("读取官方目录失败")
	}
	if groups == nil {
		groups = []Group{}
	}
	return groups, references, nil
}

type catalogColumns struct {
	name        int
	description int
}

func markdownTableValues(line string) []string {
	values := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for index := range values {
		values[index] = strings.TrimSpace(values[index])
	}
	return values
}

func isMarkdownTableDivider(line string) bool {
	for _, value := range markdownTableValues(line) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, char := range value {
			if char != '-' && char != ':' {
				return false
			}
		}
	}
	return true
}

func parseCatalogTableHeader(values []string) (catalogColumns, bool) {
	columns := catalogColumns{name: -1, description: -1}
	for index, value := range values {
		switch normalizeCatalogHeader(value) {
		case "项目", "项目名", "project", "package", "name":
			columns.name = index
		case "说明", "描述", "简介", "description", "desc", "docs", "doc", "documentation":
			columns.description = index
		}
	}
	return columns, columns.name >= 0
}

func normalizeCatalogHeader(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	return value
}

// isCatalogTableHeader keeps Markdown column labels out of the selectable
// ecosystem entries. The environment catalog uses “项目”, while older pages
// used “项目名”; both must be treated as table structure, not a package.
func isCatalogTableHeader(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "项目" || name == "项目名" || name == "project" || name == "package"
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

// repositoryInstallURL retains a tree/blob ref from the official catalog. A
// link such as /tree/v1.2.3/packages/foo must install v1.2.3, not silently
// clone the repository's moving default branch.
func repositoryInstallURL(source string) string {
	base := repositoryURL(source)
	if base == "" {
		return ""
	}
	parsed, err := url.Parse(source)
	if err != nil {
		return base
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") && validGitRef(parts[3]) {
		return base + ".git#" + parts[3]
	}
	return base + ".git"
}

func validGitRef(value string) bool {
	if value == "" || strings.HasPrefix(value, "-") || strings.Contains(value, "..") || strings.ContainsAny(value, "\\~^:?*[ \t\r\n") {
		return false
	}
	return true
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

// LoadPackageVersions returns installable versions for a catalog entry. npm
// packages use registry versions; repository-backed plugins use published
// Release tags. A source checkout without a Release must never be presented as
// a versioned plugin.
func LoadPackageVersions(name string) (PackageVersions, error) {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "git+") {
		return loadRepositoryReleases(strings.TrimPrefix(name, "git+"))
	}
	if !regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`).MatchString(name) {
		return PackageVersions{}, fmt.Errorf("该目录条目不是可查询版本的 npm 包")
	}
	endpoint := "https://registry.npmjs.org/" + url.PathEscape(name)
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Get(endpoint)
	if err != nil {
		return PackageVersions{}, fmt.Errorf("无法读取 npm 版本列表，请检查网络后重试")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return PackageVersions{}, fmt.Errorf("npm 暂时无法提供该包的版本列表")
	}
	var metadata struct {
		DistTags map[string]string `json:"dist-tags"`
		Versions map[string]any    `json:"versions"`
		Time     map[string]string `json:"time"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&metadata); err != nil {
		return PackageVersions{}, fmt.Errorf("npm 版本列表无法识别")
	}
	versions := make([]string, 0, len(metadata.Versions))
	for version := range metadata.Versions {
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool { return metadata.Time[versions[i]] > metadata.Time[versions[j]] })
	if latest := metadata.DistTags["latest"]; latest != "" {
		versions = append([]string{latest}, versions...)
	}
	return PackageVersions{Latest: metadata.DistTags["latest"], Versions: uniqueStrings(versions)}, nil
}

// loadRepositoryReleases deliberately reads release tag_name values rather
// than the repository's complete Git tag list. A Git repository often carries
// experimental tags; only a published Release is an installable plugin version.
func loadRepositoryReleases(source string) (PackageVersions, error) {
	parsed, err := url.Parse(strings.SplitN(source, "#", 2)[0])
	if err != nil || (parsed.Host != "github.com" && parsed.Host != "gitee.com") {
		return PackageVersions{}, fmt.Errorf("该插件仓库不支持读取版本 tag")
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(parsed.Path, ".git"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return PackageVersions{}, fmt.Errorf("插件仓库地址无效")
	}
	endpoint := ""
	if parsed.Host == "github.com" {
		endpoint = "https://api.github.com/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/releases?per_page=100"
	} else {
		endpoint = "https://gitee.com/api/v5/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/releases?per_page=100"
	}
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Get(endpoint)
	if err != nil {
		return PackageVersions{}, fmt.Errorf("无法读取插件 Release，请检查网络后重试")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return PackageVersions{}, fmt.Errorf("无法读取插件 Release")
	}
	var releases []struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&releases); err != nil {
		return PackageVersions{}, fmt.Errorf("插件 Release 无法识别")
	}
	versions := make([]string, 0, len(releases))
	for _, release := range releases {
		if release.TagName != "" && !release.Draft && !release.Prerelease {
			versions = append(versions, release.TagName)
		}
	}
	sort.SliceStable(versions, func(i, j int) bool { return gitTagHigher(versions[i], versions[j]) })
	versions = uniqueStrings(versions)
	if len(versions) == 0 {
		return PackageVersions{Versions: []string{}}, nil
	}
	return PackageVersions{Latest: versions[0], Versions: versions}, nil
}

// gitTagHigher keeps semantic-looking release tags ahead of arbitrary tags,
// then compares numeric components via the standard semver module.
func gitTagHigher(left, right string) bool {
	canonical := func(tag string) (string, bool) {
		tag = strings.TrimSpace(tag)
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
		if !semver.IsValid(tag) {
			return "", false
		}
		return semver.Canonical(tag), true
	}
	leftVersion, leftOK := canonical(left)
	rightVersion, rightOK := canonical(right)
	if leftOK != rightOK {
		return leftOK
	}
	if leftOK {
		return semver.Compare(leftVersion, rightVersion) > 0
	}
	return left > right
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
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
	value = strings.TrimSpace(value)
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
