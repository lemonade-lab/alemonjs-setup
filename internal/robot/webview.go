package robot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// WebViewEntry is a web UI contributed by an AlemonJS plugin belonging to the
// selected robot. A desktop.sidebar is used only as its registration point;
// setup never runs its desktop command.
type WebViewEntry struct {
	ID          string `json:"id"`
	Package     string `json:"package"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type webViewManifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Alemonjs    struct {
		Web struct {
			Root string `json:"root"`
		} `json:"web"`
		Desktop struct {
			Sidebars []struct {
				Name string `json:"name"`
			} `json:"sidebars"`
		} `json:"desktop"`
	} `json:"alemonjs"`
}

type resolvedWebView struct {
	WebViewEntry
	root string
}

func (m Manager) WebViews(root string) ([]WebViewEntry, error) {
	items, err := resolveWebViews(root)
	if err != nil {
		return nil, err
	}
	result := make([]WebViewEntry, len(items))
	for i, item := range items {
		result[i] = item.WebViewEntry
	}
	return result, nil
}

// WebViewFile resolves only files inside the registered web.root directory.
// It rejects traversal and symlink escapes before the web handler serves data.
func (m Manager) WebViewFile(root, id, requestPath string) (string, error) {
	items, err := resolveWebViews(root)
	if err != nil {
		return "", err
	}
	var entry *resolvedWebView
	for i := range items {
		if items[i].ID == id {
			entry = &items[i]
			break
		}
	}
	if entry == nil {
		return "", errors.New("未找到该机器人插件 Web 页面")
	}
	requestPath = strings.TrimPrefix(filepath.ToSlash(requestPath), "/")
	if requestPath == "" {
		requestPath = "index.html"
	}
	clean := filepath.Clean(requestPath)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("插件 Web 资源路径无效")
	}
	candidate := filepath.Join(entry.root, clean)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		// A SPA route has no file extension. Serve its entry document only.
		if filepath.Ext(clean) == "" {
			resolved = filepath.Join(entry.root, "index.html")
		} else {
			return "", errors.New("插件 Web 资源不存在")
		}
	}
	rootResolved, err := filepath.EvalSymlinks(entry.root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootResolved, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("插件 Web 资源路径无效")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("插件 Web 资源不存在")
	}
	return resolved, nil
}

func resolveWebViews(root string) ([]resolvedWebView, error) {
	project, err := projectPath(root)
	if err != nil {
		return nil, err
	}
	candidates := pluginManifestPaths(project)
	items := []resolvedWebView{}
	seen := map[string]bool{}
	for _, manifestPath := range candidates {
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		var manifest webViewManifest
		if json.Unmarshal(data, &manifest) != nil || manifest.Name == "" {
			continue
		}
		webRoot := strings.TrimSpace(manifest.Alemonjs.Web.Root)
		if webRoot == "" || len(manifest.Alemonjs.Desktop.Sidebars) == 0 {
			continue
		}
		packageDir := filepath.Dir(manifestPath)
		if filepath.IsAbs(webRoot) {
			continue
		}
		absoluteRoot := filepath.Join(packageDir, filepath.Clean(webRoot))
		resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
		if err != nil {
			continue
		}
		packageResolved, err := filepath.EvalSymlinks(packageDir)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(packageResolved, resolvedRoot)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if info, err := os.Stat(filepath.Join(resolvedRoot, "index.html")); err != nil || !info.Mode().IsRegular() {
			continue
		}
		for _, sidebar := range manifest.Alemonjs.Desktop.Sidebars {
			label := strings.TrimSpace(sidebar.Name)
			if label == "" {
				continue
			}
			id := webViewID(manifest.Name, label, packageResolved)
			if seen[id] {
				continue
			}
			seen[id] = true
			items = append(items, resolvedWebView{WebViewEntry: WebViewEntry{ID: id, Package: manifest.Name, Name: label, Description: manifest.Description}, root: resolvedRoot})
		}
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name) })
	return items, nil
}

func pluginManifestPaths(project string) []string {
	paths := []string{}
	if entries, err := os.ReadDir(filepath.Join(project, "packages")); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				paths = append(paths, filepath.Join(project, "packages", entry.Name(), "package.json"))
				// Local packages may use the standard npm scope layout:
				// packages/@scope/package. Keep the scan deliberately shallow so
				// arbitrary nested directories never become WebView candidates.
				if strings.HasPrefix(entry.Name(), "@") {
					if scoped, readErr := os.ReadDir(filepath.Join(project, "packages", entry.Name())); readErr == nil {
						for _, child := range scoped {
							if child.IsDir() {
								paths = append(paths, filepath.Join(project, "packages", entry.Name(), child.Name(), "package.json"))
							}
						}
					}
				}
			}
		}
	}
	// Only packages explicitly declared by this robot are considered. This
	// avoids exposing arbitrary transitive npm dependencies as applications.
	data, err := os.ReadFile(filepath.Join(project, "package.json"))
	if err == nil {
		var manifest struct {
			Dependencies         map[string]string `json:"dependencies"`
			DevDependencies      map[string]string `json:"devDependencies"`
			OptionalDependencies map[string]string `json:"optionalDependencies"`
		}
		if json.Unmarshal(data, &manifest) == nil {
			declared := map[string]bool{}
			for _, group := range []map[string]string{manifest.Dependencies, manifest.DevDependencies, manifest.OptionalDependencies} {
				for name := range group {
					declared[name] = true
				}
			}
			for name := range declared {
				paths = append(paths, filepath.Join(project, "node_modules", filepath.FromSlash(name), "package.json"))
			}
		}
	}
	return paths
}

func webViewID(pkg, name, directory string) string {
	sum := sha256.Sum256([]byte(pkg + "\x00" + name + "\x00" + directory))
	return hex.EncodeToString(sum[:8])
}

func (m Manager) WebViewEntry(root, id string) (WebViewEntry, error) {
	items, err := resolveWebViews(root)
	if err != nil {
		return WebViewEntry{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item.WebViewEntry, nil
		}
	}
	return WebViewEntry{}, fmt.Errorf("未找到该机器人插件 Web 页面")
}

// WebViewAPIURL resolves a plugin's relative ./api contract to the selected
// robot application. AlemonJS WebViews are often not static-only: their UI
// expects the robot's Koa API on the configured application port.
func (m Manager) WebViewAPIURL(root, id, requestPath string) (string, error) {
	if _, err := m.WebViewEntry(root, id); err != nil {
		return "", err
	}
	project, err := projectPath(root)
	if err != nil {
		return "", err
	}
	clean := filepath.ToSlash(filepath.Clean("/" + strings.TrimPrefix(requestPath, "/")))
	if clean == "/" || strings.HasPrefix(clean, "/../") {
		return "", errors.New("插件 API 路径无效")
	}
	port := 18110
	if data, readErr := os.ReadFile(filepath.Join(project, "alemon.config.yaml")); readErr == nil {
		if match := regexp.MustCompile(`(?m)^\s*serverPort\s*:\s*['\"]?(\d+)`).FindStringSubmatch(string(data)); len(match) == 2 {
			if configured, parseErr := strconv.Atoi(match[1]); parseErr == nil && configured > 0 && configured < 65536 {
				port = configured
			}
		}
	}
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/api" + clean, nil
}
