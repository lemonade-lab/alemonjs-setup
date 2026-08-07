package robot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
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

// defaultAppPort is the AlemonJS application port used when alemon.config.yaml
// declares no serverPort.
const defaultAppPort = 18110

// AppPortInfo describes the robot's application port: the configured value
// (serverPort in alemon.config.yaml) and whether it was explicitly declared.
type AppPortInfo struct {
	Port       int  `json:"port"`
	Configured bool `json:"configured"`
}

// AppPort returns the robot's application port from alemon.config.yaml
// (serverPort) and whether it was explicitly configured.
func (Manager) AppPort(root string) (AppPortInfo, error) {
	project, err := projectPath(root)
	if err != nil {
		return AppPortInfo{}, err
	}
	data, err := os.ReadFile(filepath.Join(project, "alemon.config.yaml"))
	if err != nil {
		return AppPortInfo{Port: defaultAppPort}, nil
	}
	if match := regexp.MustCompile(`(?m)^\s*serverPort\s*:\s*['\"]?(\d+)`).FindStringSubmatch(string(data)); len(match) == 2 {
		if configured, parseErr := strconv.Atoi(match[1]); parseErr == nil && configured > 0 && configured < 65536 {
			return AppPortInfo{Port: configured, Configured: true}, nil
		}
	}
	return AppPortInfo{Port: defaultAppPort}, nil
}

// SaveAppPort writes serverPort into alemon.config.yaml, replacing an existing
// value or appending a new one.
func (Manager) SaveAppPort(root string, port int) (Result, error) {
	if port < 1 || port > 65535 {
		return Result{}, errors.New("应用端口应在 1-65535 之间")
	}
	project, err := projectPath(root)
	if err != nil {
		return Result{}, err
	}
	configFile := filepath.Join(project, "alemon.config.yaml")
	content, err := os.ReadFile(configFile)
	if err != nil && !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("无法读取运行配置：%w", err)
	}
	text := string(content)
	value := "serverPort: " + strconv.Itoa(port)
	pattern := regexp.MustCompile(`(?m)^\s*serverPort\s*:\s*['\"]?\d+['\"]?\s*$`)
	if pattern.MatchString(text) {
		text = pattern.ReplaceAllString(text, value)
	} else {
		text = strings.TrimRight(text, "\n")
		if text != "" {
			text += "\n"
		}
		text += value + "\n"
	}
	if err := os.WriteFile(configFile, []byte(text), 0644); err != nil {
		if permissionError(err) {
			return Result{}, permissionAdvice("保存应用端口")
		}
		return Result{}, fmt.Errorf("无法保存应用端口：%w", err)
	}
	return Result{Path: configFile, Output: "应用端口已设置为 " + strconv.Itoa(port) + "。"}, nil
}

// EnabledApps reads the alemon.config.yaml apps array. AlemonJS loads each name
// in apps as a plugin package; item is the npm name of a local package.
func (Manager) EnabledApps(root string) ([]string, error) {
	project, err := projectPath(root)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(filepath.Join(project, "alemon.config.yaml"))
	if err != nil {
		return []string{}, nil
	}
	var config map[string]any
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("无法解析运行配置：%w", err)
	}
	switch apps := config["apps"].(type) {
	case []any:
		result := make([]string, 0, len(apps))
		for _, item := range apps {
			if name, ok := item.(string); ok && name != "" {
				result = append(result, name)
			}
		}
		return result, nil
	case []string:
		return apps, nil
	case nil:
		return []string{}, nil
	default:
		return []string{}, nil
	}
}

// SetAppEnabled adds or removes a local package's npm name from the
// alemon.config.yaml apps array, controlling whether the robot loads it.
func (Manager) SetAppEnabled(root, packageName string, enabled bool) (Result, error) {
	if strings.TrimSpace(packageName) == "" {
		return Result{}, errors.New("请选择要启动的本地包")
	}
	project, err := projectPath(root)
	if err != nil {
		return Result{}, err
	}
	configFile := filepath.Join(project, "alemon.config.yaml")
	content, err := os.ReadFile(configFile)
	if err != nil && !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("无法读取运行配置：%w", err)
	}
	var config map[string]any
	if len(strings.TrimSpace(string(content))) > 0 {
		if err := yaml.Unmarshal(content, &config); err != nil {
			return Result{}, fmt.Errorf("无法解析运行配置：%w", err)
		}
	}
	if config == nil {
		config = map[string]any{}
	}
	apps := []string{}
	switch existing := config["apps"].(type) {
	case []any:
		for _, item := range existing {
			if name, ok := item.(string); ok && name != "" {
				apps = append(apps, name)
			}
		}
	case []string:
		apps = append(apps, existing...)
	}
	found := false
	filtered := apps[:0]
	for _, name := range apps {
		if name == packageName {
			found = true
			continue
		}
		filtered = append(filtered, name)
	}
	if enabled && !found {
		filtered = append(filtered, packageName)
	}
	config["apps"] = filtered
	encoded, err := yaml.Marshal(config)
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(configFile, encoded, 0644); err != nil {
		if permissionError(err) {
			return Result{}, permissionAdvice("保存运行配置")
		}
		return Result{}, fmt.Errorf("无法保存运行配置：%w", err)
	}
	state := "已停用"
	if enabled {
		state = "已启用"
	}
	return Result{Path: configFile, Output: packageName + " " + state + "。"}, nil
}

// AppPortReachable probes whether the robot's application is actually serving
// on its configured port. The browser cannot reach 127.0.0.1 across the dev
// proxy/CSP, so the backend performs the health check before opening the app.
func (m Manager) AppPortReachable(root string) (bool, int, error) {
	info, err := m.AppPort(root)
	if err != nil {
		return false, 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(info.Port), nil)
	if err != nil {
		return false, info.Port, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false, info.Port, nil
	}
	defer response.Body.Close()
	// Any HTTP response (even 404/500) means a server is listening on the port.
	return true, info.Port, nil
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
