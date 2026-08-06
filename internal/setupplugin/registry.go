// Package setupplugin discovers declarative extensions that add system
// controls to alx. Discovery never executes plugin code: a plugin becomes
// runnable only after a later, explicitly approved action protocol is added.
package setupplugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

const manifestName = "alx.json"
const maxManifestSize = 64 * 1024

var validID = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

// Page is a secondary navigation item contributed by a Setup plugin.
type Page struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Navigation controls where the plugin appears in the global function rail.
type Navigation struct {
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
	Order int    `json:"order,omitempty"`
}

// Action is a user-visible operation supplied by a Setup plugin. Dangerous
// actions always require a second explicit confirmation from the UI/API.
type Action struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Description string  `json:"description,omitempty"`
	Confirm     bool    `json:"confirm,omitempty"`
	Page        string  `json:"page,omitempty"`
	Fields      []Field `json:"fields,omitempty"`
}

type Field struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Options []Option `json:"options,omitempty"`
	Default string   `json:"default,omitempty"`
}

type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Plugin is intentionally declarative. It is safe to list and render because
// no file from its directory is executed during discovery.
type Plugin struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description,omitempty"`
	Platforms   []string          `json:"platforms,omitempty"`
	Navigation  Navigation        `json:"navigation"`
	Pages       []Page            `json:"pages"`
	Actions     []Action          `json:"actions,omitempty"`
	Runtime     string            `json:"runtime,omitempty"`
	Entry       map[string]string `json:"entry,omitempty"`
	Development *RuntimeSpec      `json:"development,omitempty"`
	Runnable    bool              `json:"runnable"`
	Enabled     bool              `json:"enabled"`
	Source      string            `json:"source,omitempty"`
}

// RuntimeSpec is an optional development fallback. Release plugins should use
// a compiled binary. A source runner may be kept here so contributors can run
// a plugin from a checkout without first producing every platform binary.
type RuntimeSpec struct {
	Runtime string            `json:"runtime"`
	Entry   map[string]string `json:"entry"`
}

// Registry scans immediate child directories in order. Earlier roots win on
// duplicate IDs, allowing a user-installed plugin to override a bundled one.
type Registry struct {
	roots     []string
	statePath string
}

func NewRegistry(roots ...string) Registry {
	statePath := ""
	if len(roots) == 0 {
		roots = defaultRoots()
		statePath = defaultStatePath()
	}
	return Registry{roots: uniqueRoots(roots), statePath: statePath}
}

func defaultRoots() []string {
	roots := make([]string, 0, 3)
	if executable, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Join(filepath.Dir(executable), "plugins"))
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, filepath.Join(cwd, "plugins"))
	}
	if config, err := os.UserConfigDir(); err == nil {
		roots = append(roots, filepath.Join(config, "alx", "plugins"))
	}
	return roots
}

func defaultStatePath() string {
	if config, err := os.UserConfigDir(); err == nil {
		return filepath.Join(config, "alx", "setup-plugins.json")
	}
	return ""
}

func uniqueRoots(roots []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(roots))
	for _, root := range roots {
		root = filepath.Clean(root)
		if root == "." || seen[root] {
			continue
		}
		seen[root] = true
		result = append(result, root)
	}
	return result
}

// List returns valid plugins only. A malformed third-party directory must not
// stop the app or hide other plugin entries.
func (r Registry) List() []Plugin {
	return r.list(false)
}

// All includes disabled plugins so the manager can offer a deliberate
// re-enable action, while List remains the source for the live navigation.
func (r Registry) All() []Plugin {
	return r.list(true)
}

func (r Registry) list(includeDisabled bool) []Plugin {
	items := make([]Plugin, 0)
	seen := map[string]bool{}
	disabled := r.disabled()
	for _, root := range r.roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			plugin, err := load(filepath.Join(root, entry.Name()))
			if err != nil || seen[plugin.ID] || !supportsCurrentPlatform(plugin.Platforms) {
				continue
			}
			plugin.Enabled = !disabled[plugin.ID]
			seen[plugin.ID] = true
			if !includeDisabled && !plugin.Enabled {
				continue
			}
			items = append(items, plugin)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Navigation.Order != items[j].Navigation.Order {
			return items[i].Navigation.Order < items[j].Navigation.Order
		}
		return items[i].Navigation.Label < items[j].Navigation.Label
	})
	return items
}

type disabledState struct {
	Disabled []string `json:"disabled"`
}

func (r Registry) disabled() map[string]bool {
	items := map[string]bool{}
	if r.statePath == "" {
		return items
	}
	data, err := os.ReadFile(r.statePath)
	if err != nil || len(data) > maxManifestSize {
		return items
	}
	var state disabledState
	if json.Unmarshal(data, &state) != nil {
		return items
	}
	for _, id := range state.Disabled {
		if validID.MatchString(id) {
			items[id] = true
		}
	}
	return items
}

// SetEnabled is a reversible uninstall: the plugin's files are left intact,
// but it disappears from the active function rail and cannot run actions.
func (r Registry) SetEnabled(id string, enabled bool) error {
	if !validID.MatchString(id) {
		return errors.New("无效的 Setup 插件标识")
	}
	found := false
	for _, plugin := range r.All() {
		if plugin.ID == id {
			found = true
			break
		}
	}
	if !found {
		return errors.New("未找到 Setup 插件")
	}
	if r.statePath == "" {
		return errors.New("当前运行环境不支持保存插件启用状态")
	}
	disabled := r.disabled()
	if enabled {
		delete(disabled, id)
	} else {
		disabled[id] = true
	}
	ids := make([]string, 0, len(disabled))
	for item := range disabled {
		ids = append(ids, item)
	}
	sort.Strings(ids)
	data, err := json.Marshal(disabledState{Disabled: ids})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.statePath), 0755); err != nil {
		return err
	}
	temporary := r.statePath + ".new"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		return err
	}
	return os.Rename(temporary, r.statePath)
}

// Find returns one currently discoverable plugin. It rescans deliberately so
// adding a folder is reflected without restarting alx.
func (r Registry) Find(id string) (Plugin, error) {
	for _, plugin := range r.List() {
		if plugin.ID == id {
			return plugin, nil
		}
	}
	return Plugin{}, errors.New("未找到已加载的 Setup 插件")
}

type request struct {
	Protocol string            `json:"protocol"`
	Method   string            `json:"method"`
	Action   string            `json:"action"`
	Params   map[string]string `json:"params,omitempty"`
}

type response struct {
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Run invokes a declared plugin entry using the stable JSON stdin/stdout
// contract. The plugin receives no shell and no inherited user input.
func (r Registry) Run(id, actionID string, params map[string]string, confirmed bool) (string, error) {
	plugin, err := r.Find(id)
	if err != nil {
		return "", err
	}
	var action Action
	found := false
	for _, item := range plugin.Actions {
		if item.ID == actionID {
			action, found = item, true
			break
		}
	}
	if !found {
		return "", errors.New("该 Setup 插件没有声明此操作")
	}
	if action.Confirm && !confirmed {
		return "", fmt.Errorf("“%s”会修改本机系统，请确认后继续", action.Label)
	}
	entry, err := plugin.entryPath()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(request{Protocol: "alx.setup/v1", Method: "run", Action: actionID, Params: params})
	if err != nil {
		return "", err
	}
	command := exec.Command(entry.name, entry.args...)
	command.Dir = plugin.Source
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("插件入口无法启动：未检测到 %s。请先安装插件所需运行环境后重试", entry.name)
		}
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("插件执行失败：%s", message)
	}
	var result response
	if err := json.Unmarshal(output, &result); err != nil {
		return "", errors.New("插件返回格式无效；请使用 alx.setup/v1 JSON 协议")
	}
	if result.Error != "" {
		return result.Output, errors.New(result.Error)
	}
	if result.Output == "" {
		result.Output = "插件操作已完成。"
	}
	return result.Output, nil
}

type executable struct {
	name string
	args []string
}

func (p Plugin) entryPath() (executable, error) {
	entry, err := p.runtimeEntry(p.Runtime, p.Entry)
	if err == nil {
		return entry, nil
	}
	if p.Development != nil {
		if fallback, fallbackErr := p.runtimeEntry(p.Development.Runtime, p.Development.Entry); fallbackErr == nil {
			return fallback, nil
		}
	}
	return executable{}, err
}

func (p Plugin) runtimeEntry(runtimeName string, entries map[string]string) (executable, error) {
	if runtimeName == "" {
		runtimeName = "binary"
	}
	if runtimeName == "go" {
		relative := entries["go"]
		if relative == "" || filepath.IsAbs(relative) || strings.HasPrefix(filepath.Clean(relative), ".."+string(filepath.Separator)) {
			return executable{}, errors.New("Go 插件缺少位于插件目录内的入口文件")
		}
		return executable{name: "go", args: []string{"run", filepath.Join(p.Source, relative)}}, nil
	}
	key := runtime.GOOS + "-" + runtime.GOARCH
	relative := entries[key]
	if relative == "" {
		relative = entries[runtime.GOOS]
	}
	if relative == "" {
		return executable{}, errors.New("此插件没有适用于当前系统的执行器")
	}
	if filepath.IsAbs(relative) || strings.HasPrefix(filepath.Clean(relative), ".."+string(filepath.Separator)) {
		return executable{}, errors.New("插件执行器路径必须位于插件目录内")
	}
	path := filepath.Join(p.Source, relative)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return executable{}, errors.New("插件执行器不可用")
	}
	if runtimeName == "node" {
		return executable{name: "node", args: []string{path}}, nil
	}
	return executable{name: path}, nil
}

func load(directory string) (Plugin, error) {
	path := filepath.Join(directory, manifestName)
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxManifestSize {
		return Plugin{}, errors.New("invalid setup plugin manifest")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Plugin{}, err
	}
	var plugin Plugin
	if err := json.Unmarshal(data, &plugin); err != nil {
		return Plugin{}, err
	}
	if !validID.MatchString(plugin.ID) || strings.TrimSpace(plugin.Name) == "" || strings.TrimSpace(plugin.Version) == "" {
		return Plugin{}, errors.New("setup plugin requires id, name and version")
	}
	if plugin.Navigation.Label == "" {
		plugin.Navigation.Label = plugin.Name
	}
	if plugin.Navigation.Icon == "" {
		plugin.Navigation.Icon = "◈"
	}
	if len(plugin.Pages) == 0 {
		plugin.Pages = []Page{{ID: "overview", Label: "概览", Description: "查看此系统能力的安装与运行状态。"}}
	}
	if !validRuntime(plugin.Runtime) {
		return Plugin{}, errors.New("setup plugin runtime must be binary, node or go")
	}
	if plugin.Development != nil && (!validRuntime(plugin.Development.Runtime) || len(plugin.Development.Entry) == 0) {
		return Plugin{}, errors.New("setup plugin development runner is invalid")
	}
	seenPages := map[string]bool{}
	for _, page := range plugin.Pages {
		if !validID.MatchString(page.ID) || strings.TrimSpace(page.Label) == "" || seenPages[page.ID] {
			return Plugin{}, fmt.Errorf("invalid setup plugin page")
		}
		seenPages[page.ID] = true
	}
	seenActions := map[string]bool{}
	for _, action := range plugin.Actions {
		if !validID.MatchString(action.ID) || strings.TrimSpace(action.Label) == "" || seenActions[action.ID] {
			return Plugin{}, errors.New("invalid setup plugin action")
		}
		seenActions[action.ID] = true
	}
	plugin.Runnable = len(plugin.Actions) > 0 && (len(plugin.Entry) > 0 || plugin.Development != nil)
	plugin.Source = directory
	return plugin, nil
}

func validRuntime(value string) bool {
	return value == "" || value == "binary" || value == "node" || value == "go"
}

func supportsCurrentPlatform(platforms []string) bool {
	if len(platforms) == 0 {
		return true
	}
	for _, platform := range platforms {
		if platform == runtime.GOOS {
			return true
		}
	}
	return false
}
