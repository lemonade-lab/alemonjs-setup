// Package setupplugin discovers setup plugins that add system controls to
// alx. A plugin is a static web UI (web.root) plus an optional executor that
// runs system operations the web UI requests through a generic forward. The
// declarative pages/actions manifest model has been removed: the web UI is the
// plugin's interface. Discovery never executes plugin code.
package setupplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const manifestName = "alx.json"
const maxManifestSize = 64 * 1024
const onlineIndexURL = "https://raw.githubusercontent.com/lemonade-lab/alemonjs.dev/main/docs/apps-x.md"
const installTimeout = 3 * time.Minute

var validID = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
var onlineRepository = regexp.MustCompile(`(?m)^\s*\[[^\]]+\]:\s*(https://github\.com/lemonade-lab/([A-Za-z0-9_.-]+))\s*$`)

// Navigation controls where the plugin appears in the global function rail.
type Navigation struct {
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
	Order int    `json:"order,omitempty"`
}

// Plugin is intentionally declarative. It is safe to list and render because
// no file from its directory is executed during discovery. A plugin is usable
// only when it declares a web root (its UI) and has an executor.
type Plugin struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description,omitempty"`
	Platforms   []string          `json:"platforms,omitempty"`
	Navigation  Navigation        `json:"navigation"`
	Runtime     string            `json:"runtime,omitempty"`
	Entry       map[string]string `json:"entry,omitempty"`
	Development *RuntimeSpec      `json:"development,omitempty"`
	Web         *WebSpec          `json:"web,omitempty"`
	Runnable    bool              `json:"runnable"`
	Enabled     bool              `json:"enabled"`
	Online      bool              `json:"online,omitempty"`
	Source      string            `json:"source,omitempty"`
}

// RuntimeSpec is an optional development fallback. Release plugins should use
// a compiled binary. A source runner may be kept here so contributors can run
// a plugin from a checkout without first producing every platform binary.
type RuntimeSpec struct {
	Runtime string            `json:"runtime"`
	Entry   map[string]string `json:"entry"`
}

// WebSpec declares the plugin's web UI directory inside the plugin folder. alx
// serves it same-origin so the UI can call the plugin's action forward API
// directly. Only installed, enabled plugins have their web root served.
type WebSpec struct {
	Root string `json:"root"`
}

// Registry scans immediate child directories in order. Earlier roots win on
// duplicate IDs, allowing a user-installed plugin to override a bundled one.
// Results are cached and refreshed by Rescan/StartWatch so hot-plugging a
// plugin directory is reflected without restarting alx.
type Registry struct {
	mu                sync.RWMutex
	roots             []string
	statePath         string
	onlineIndexURL    string
	httpClient        *http.Client
	onlineManifestURL func(string) string
	cloneURL          func(string) string
	cached            []Plugin
	revision          uint64
	loaded            bool
	lastFingerprint   string
	listeners         map[chan struct{}]struct{}
}

// Subscribe returns a channel that receives a signal whenever the cached plugin
// set changes (revision bumps). Consumers that render the plugin list use this
// to refresh without polling the whole list. The signal is coalesced: a slow
// consumer may miss intermediate changes but is always woken for the latest.
func (r *Registry) Subscribe() chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listeners == nil {
		r.listeners = map[chan struct{}]struct{}{}
	}
	ch := make(chan struct{}, 1)
	r.listeners[ch] = struct{}{}
	return ch
}

// Unsubscribe stops delivering change signals to a channel from Subscribe.
func (r *Registry) Unsubscribe(ch chan struct{}) {
	r.mu.Lock()
	delete(r.listeners, ch)
	r.mu.Unlock()
}

func NewRegistry(roots ...string) *Registry {
	if len(roots) == 0 {
		roots = defaultRoots()
		return &Registry{
			roots:             uniqueRoots(roots),
			statePath:         defaultStatePath(),
			onlineIndexURL:    onlineIndexURL,
			httpClient:        &http.Client{Timeout: 5 * time.Second},
			onlineManifestURL: defaultOnlineManifestURL,
			cloneURL:          defaultCloneURL,
		}
	}
	return &Registry{roots: uniqueRoots(roots)}
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

// List returns valid, enabled plugins from the cached snapshot.
func (r *Registry) List() []Plugin {
	r.ensureLoaded()
	items := r.snapshot()
	enabled := items[:0]
	for _, plugin := range items {
		if plugin.Enabled {
			enabled = append(enabled, plugin)
		}
	}
	return enabled
}

// All includes disabled plugins so the manager can offer a deliberate
// re-enable action, while List remains the source for the live navigation.
func (r *Registry) All() []Plugin {
	r.ensureLoaded()
	return r.snapshot()
}

// Find returns one currently discoverable plugin from the cache.
func (r *Registry) Find(id string) (Plugin, error) {
	for _, plugin := range r.All() {
		if plugin.ID == id {
			return plugin, nil
		}
	}
	return Plugin{}, errors.New("未找到已加载的 Setup 插件")
}

// Revision returns the cache revision, bumped whenever a rescan changes the
// plugin set. Poll it (or the plugin list) for hot-plug detection.
func (r *Registry) Revision() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.revision
}

func (r *Registry) ensureLoaded() {
	r.mu.RLock()
	loaded := r.loaded
	r.mu.RUnlock()
	if loaded {
		return
	}
	r.Rescan()
}

func (r *Registry) snapshot() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Plugin, len(r.cached))
	copy(items, r.cached)
	return items
}

// Rescan recomputes the full plugin set and replaces the cache. It bumps the
// revision only when the set actually changed, so watchers can cheaply detect
// real changes.
func (r *Registry) Rescan() {
	next := r.compute()
	r.mu.Lock()
	changed := !pluginSetsEqual(r.cached, next)
	r.cached = next
	r.loaded = true
	if changed {
		r.revision++
		// Wake subscribers non-blockingly so a full rescan never blocks on a
		// slow SSE consumer. The 1-buffer channel coalesces bursts.
		for ch := range r.listeners {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
	r.mu.Unlock()
}

func pluginSetsEqual(a, b []Plugin) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !pluginsEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func pluginsEqual(a, b Plugin) bool {
	return a.ID == b.ID && a.Name == b.Name && a.Version == b.Version &&
		a.Description == b.Description && a.Runnable == b.Runnable &&
		a.Enabled == b.Enabled && a.Online == b.Online &&
		strings.Join(a.Platforms, ",") == strings.Join(b.Platforms, ",") &&
		a.Navigation.Label == b.Navigation.Label && a.Navigation.Icon == b.Navigation.Icon &&
		a.Navigation.Order == b.Navigation.Order && webRootEqual(a.Web, b.Web) &&
		entryEqual(a.Entry, b.Entry) && entryEqual(a.DevelopmentEntry(), b.DevelopmentEntry())
}

func (p Plugin) DevelopmentEntry() map[string]string {
	if p.Development != nil {
		return p.Development.Entry
	}
	return nil
}

func webRootEqual(a, b *WebSpec) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Root == b.Root
}

func entryEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func (r *Registry) compute() []Plugin {
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
			items = append(items, plugin)
		}
	}
	for _, plugin := range r.onlinePlugins() {
		if seen[plugin.ID] || !supportsCurrentPlatform(plugin.Platforms) {
			continue
		}
		plugin.Enabled = !disabled[plugin.ID]
		seen[plugin.ID] = true
		items = append(items, plugin)
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

func (r *Registry) disabled() map[string]bool {
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
// but it disappears from the active function rail and its web UI is no longer
// served. The cache is refreshed immediately.
func (r *Registry) SetEnabled(id string, enabled bool) error {
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
	if err := os.Rename(temporary, r.statePath); err != nil {
		return err
	}
	r.Rescan()
	return nil
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

// Run forwards a web UI request to the plugin executor using the stable JSON
// stdin/stdout contract. The plugin receives no shell and no inherited user
// input. The executor itself whitelists the action names it supports and
// validates every parameter; there is no manifest-level action declaration
// anymore. "confirmed" is accepted for API compatibility but the web UI owns
// its own confirmation UX.
func (r *Registry) Run(id, actionID string, params map[string]string, confirmed bool) (string, error) {
	plugin, err := r.Find(id)
	if err != nil {
		return "", err
	}
	if plugin.Online {
		return "", errors.New("在线系统插件尚未安装，不能执行远程代码")
	}
	if !plugin.Enabled {
		return "", errors.New("该 Setup 插件已停用")
	}
	if !plugin.Runnable {
		return "", errors.New("该 Setup 插件缺少可用的执行器")
	}
	entry, err := plugin.entryPath()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(request{Protocol: "alx/v1", Method: "run", Action: actionID, Params: params})
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
		return "", errors.New("插件返回格式无效；请使用 alx/v1 JSON 协议")
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

// WebRoot resolves the plugin's static web directory and verifies it stays
// inside the plugin directory even if intermediate components are symlinks.
func (p Plugin) WebRoot() (string, error) {
	if p.Web == nil || strings.TrimSpace(p.Web.Root) == "" {
		return "", errors.New("此插件未提供静态 Web 界面")
	}
	root := filepath.Join(p.Source, filepath.FromSlash(p.Web.Root))
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", errors.New("插件 Web 目录不存在或不可访问")
	}
	sourceResolved, err := filepath.EvalSymlinks(p.Source)
	if err != nil {
		return "", errors.New("插件目录不可访问")
	}
	rel, err := filepath.Rel(sourceResolved, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("插件 Web 目录越界")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("插件 Web 目录不可用")
	}
	return resolved, nil
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
	return decodeManifest(data, directory)
}

func decodeManifest(data []byte, source string) (Plugin, error) {
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
	// Web UI is the plugin's interface; a manifest without a web root is not a
	// usable setup plugin.
	if plugin.Web == nil {
		return Plugin{}, errors.New("setup plugin requires a web root")
	}
	root := filepath.ToSlash(strings.TrimSpace(plugin.Web.Root))
	if root == "" || filepath.IsAbs(root) || root == ".." {
		return Plugin{}, errors.New("setup plugin web root must be a directory inside the plugin")
	}
	// Reject any ".." path component before cleaning hides it.
	for _, component := range strings.Split(root, "/") {
		if component == ".." {
			return Plugin{}, errors.New("setup plugin web root must be a directory inside the plugin")
		}
	}
	clean := filepath.Clean(root)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return Plugin{}, errors.New("setup plugin web root must be a directory inside the plugin")
	}
	plugin.Web.Root = root
	if !validRuntime(plugin.Runtime) {
		return Plugin{}, errors.New("setup plugin runtime must be binary, node or go")
	}
	if plugin.Development != nil && (!validRuntime(plugin.Development.Runtime) || len(plugin.Development.Entry) == 0) {
		return Plugin{}, errors.New("setup plugin development runner is invalid")
	}
	plugin.Runnable = len(plugin.Entry) > 0 || plugin.Development != nil
	plugin.Source = source
	return plugin, nil
}

func defaultOnlineManifestURL(repository string) string {
	name := strings.TrimPrefix(repository, "https://github.com/lemonade-lab/")
	return "https://raw.githubusercontent.com/lemonade-lab/" + name + "/main/" + manifestName
}

func defaultCloneURL(repository string) string {
	name := strings.TrimPrefix(repository, "https://github.com/lemonade-lab/")
	return "https://github.com/lemonade-lab/" + name + ".git"
}

// onlinePlugins reads the curated Apps-X index. Only repositories owned by
// lemonade-lab are accepted, so a documentation edit cannot turn discovery
// into an arbitrary URL fetch. Online manifests are deliberately read-only:
// they render in the manager but must be installed locally before execution.
func (r *Registry) onlinePlugins() []Plugin {
	if r.onlineIndexURL == "" || r.httpClient == nil || r.onlineManifestURL == nil {
		return nil
	}
	index, err := r.readOnlineFile(r.onlineIndexURL)
	if err != nil {
		return nil
	}
	items := make([]Plugin, 0)
	seen := map[string]bool{}
	for _, match := range onlineRepository.FindAllStringSubmatch(string(index), -1) {
		repository := match[1]
		manifest, err := r.readOnlineFile(r.onlineManifestURL(repository))
		if err != nil {
			continue
		}
		plugin, err := decodeManifest(manifest, repository)
		if err != nil || seen[plugin.ID] {
			continue
		}
		plugin.Online = true
		plugin.Runnable = false
		seen[plugin.ID] = true
		items = append(items, plugin)
	}
	return items
}

// Install clones an online plugin's repository into a plugin root and rescans,
// turning the read-only online entry into a local, loaded plugin. Only
// lemonade-lab repositories discovered from the Apps-X index are accepted. The
// clone is shallow and bounded by installTimeout; a partial clone is removed on
// failure so a retry starts clean.
func (r *Registry) Install(id string) (Plugin, error) {
	if !validID.MatchString(id) {
		return Plugin{}, errors.New("无效的 Setup 插件标识")
	}
	online := r.onlinePlugin(id)
	if online == nil {
		return Plugin{}, errors.New("未找到可安装的在线 Setup 插件")
	}
	if online.Source == "" || !onlineRepository.MatchString(online.Source) {
		return Plugin{}, errors.New("在线插件仓库来源不受支持")
	}
	root, err := r.installRoot()
	if err != nil {
		return Plugin{}, err
	}
	name := strings.TrimPrefix(online.Source, "https://github.com/lemonade-lab/")
	if !validID.MatchString(name) {
		return Plugin{}, errors.New("在线插件仓库名无效")
	}
	target := filepath.Join(root, name)
	if _, err := os.Lstat(target); err == nil {
		return Plugin{}, errors.New("该插件已经安装")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Plugin{}, errors.New("无法创建插件安装目录：" + err.Error())
	}
	clone := r.cloneURL
	if clone == nil {
		clone = defaultCloneURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "-q", clone(online.Source), target)
	if output, err := command.CombinedOutput(); err != nil {
		_ = os.RemoveAll(target)
		message := strings.TrimSpace(string(output))
		if errors.Is(err, exec.ErrNotFound) {
			return Plugin{}, errors.New("未检测到 git，无法在线安装插件")
		}
		if message == "" {
			message = err.Error()
		}
		return Plugin{}, fmt.Errorf("在线安装插件失败：%s", message)
	}
	r.Rescan()
	installed, err := r.Find(id)
	if err != nil {
		return Plugin{}, errors.New("插件已下载，但加载失败；请检查插件目录 " + target)
	}
	return installed, nil
}

// onlinePlugin returns one currently discoverable online-only plugin, falling
// back to a fresh index fetch if the cache has not been populated.
func (r *Registry) onlinePlugin(id string) *Plugin {
	for _, plugin := range r.All() {
		if plugin.Online && plugin.ID == id {
			return &plugin
		}
	}
	return nil
}

// installRoot picks where an online plugin is cloned. The user-level root (where
// enable state is stored) is preferred when it is one of the scan roots, so an
// install lands in a directory alx owns; otherwise the first root a directory
// can be created in is used.
func (r *Registry) installRoot() (string, error) {
	preferred := ""
	if config, err := os.UserConfigDir(); err == nil {
		preferred = filepath.Join(config, "alx", "plugins")
	}
	hasPreferred := false
	for _, root := range r.roots {
		if root == preferred {
			hasPreferred = true
			break
		}
	}
	ordered := make([]string, 0, len(r.roots))
	if hasPreferred {
		ordered = append(ordered, preferred)
	}
	for _, root := range r.roots {
		if root == preferred {
			continue
		}
		ordered = append(ordered, root)
	}
	for _, root := range ordered {
		if err := os.MkdirAll(root, 0o755); err == nil {
			return root, nil
		}
	}
	return "", errors.New("没有可写入的插件安装目录")
}

func (r *Registry) readOnlineFile(url string) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := r.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("online plugin request returned %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxManifestSize+1))
	if err != nil || len(data) > maxManifestSize {
		return nil, errors.New("online plugin document is unavailable")
	}
	return data, nil
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

// Watcher polls the plugin roots and rescans when a manifest or the plugin
// directory listing changes, giving hot-plug without a filesystem event
// library. The poll is cheap: it only stats alx.json files and lists each
// root's immediate children.
type Watcher struct {
	registry *Registry
	stop     chan struct{}
	done     chan struct{}
}

// StartWatch begins polling at interval. Call Stop to end it. Interval 0
// disables the poller (the cache is still filled by the first List/All call).
func (r *Registry) StartWatch(interval time.Duration) *Watcher {
	r.ensureLoaded()
	watcher := &Watcher{registry: r, stop: make(chan struct{}), done: make(chan struct{})}
	if interval <= 0 {
		close(watcher.done)
		return watcher
	}
	go watcher.loop(interval)
	return watcher
}

func (w *Watcher) loop(interval time.Duration) {
	defer close(w.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			if w.registry.fingerprintChanged() {
				w.registry.Rescan()
			}
		}
	}
}

// Stop ends the polling goroutine and waits for it to exit.
func (w *Watcher) Stop() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
	<-w.done
}

// fingerprintChanged stats the manifest files and directory listings. It
// returns true when the previous fingerprint differs.
func (r *Registry) fingerprintChanged() bool {
	next := r.fingerprint()
	r.mu.Lock()
	defer r.mu.Unlock()
	if next != r.lastFingerprint {
		r.lastFingerprint = next
		return true
	}
	return false
}

func (r *Registry) fingerprint() string {
	var builder strings.Builder
	for _, root := range r.roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			builder.WriteString(root)
			builder.WriteByte(0)
			builder.WriteString(entry.Name())
			builder.WriteByte(0)
			if info, statErr := os.Stat(filepath.Join(root, entry.Name(), manifestName)); statErr == nil {
				builder.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))
				builder.WriteByte(':')
				builder.WriteString(strconv.FormatInt(info.Size(), 10))
			}
			builder.WriteByte('\n')
		}
	}
	if r.statePath != "" {
		if info, statErr := os.Stat(r.statePath); statErr == nil {
			builder.WriteString("state:")
			builder.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))
		}
	}
	return builder.String()
}
