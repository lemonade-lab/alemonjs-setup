// Package web provides the embedded setup guide and its HTTP API.
package web

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"alemonjs-setup/internal/access"
	"alemonjs-setup/internal/ai"
	"alemonjs-setup/internal/catalog"
	"alemonjs-setup/internal/project"
	"alemonjs-setup/internal/releases"
	"alemonjs-setup/internal/robot"
	"alemonjs-setup/internal/setupplugin"
	"alemonjs-setup/internal/system"
)

type goal struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Steps       []string `json:"steps"`
	DownloadURL string   `json:"downloadUrl,omitempty"`
	Mirrors     []mirror `json:"mirrors,omitempty"`
}

type mirror struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type task struct {
	ID        string    `json:"id"`
	GoalID    string    `json:"goalId"`
	Status    string    `json:"status"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
}

type operationTask struct {
	ID         string     `json:"id"`
	Root       string     `json:"root"`
	Action     string     `json:"action"`
	Status     string     `json:"status"`
	Output     string     `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type developmentProcess struct {
	TaskID  string
	Command *exec.Cmd
}

type server struct {
	version        string
	assets         fs.FS
	static         http.Handler
	checker        *system.Checker
	creator        *project.Creator
	robots         robot.Manager
	plugins        setupplugin.Registry
	auth           *access.Manager
	ai             *ai.Manager
	mu             sync.RWMutex
	tasks          []task
	operations     []operationTask
	development    map[string]developmentProcess
	stopping       map[string]bool
	requestID      atomic.Uint64
	directoryRoots []string
}

var goals = []goal{
	{ID: "install", Title: "安装机器人", Description: "用推荐默认配置，快速安装一个可以运行的 AlemonJS 机器人。", Steps: []string{"环境检查", "机器人名称与位置", "确认安装"}},
	{ID: "manage", Title: "管理机器人", Description: "管理已有机器人项目的配置、依赖与运行方式。", Steps: []string{"打开机器人管理"}},
	{ID: "develop", Title: "开发机器人", Description: "创建一个可按需配置的 AlemonJS 开发项目。", Steps: []string{"环境检查", "项目名称", "开发语言", "代码规范", "版本管理", "本地运行", "包管理器", "开发能力包", "图片开发", "样式方案", "开发技能", "确认创建"}},
	{ID: "desktop", Title: "安装桌面版", Description: "下载 AlemonDesk。", Steps: []string{"选择下载镜像", "下载桌面版"}, Mirrors: githubMirrors("alemondesk")},
	{ID: "mobile", Title: "安装手机版", Description: "下载 AlemonApp Android 安装包。", Steps: []string{"下载 Android 安装包"}, DownloadURL: "https://download.alemonjs.com/application/alemonapp/app-universal-release.apk"},
	{ID: "web", Title: "部署 Web 版", Description: "部署 AlemonGo。", Steps: []string{"选择部署方式", "环境检查", "快速启动"}, Mirrors: githubMirrors("alemongo")},
}

func githubMirrors(repository string) []mirror {
	url := "https://github.com/lemonade-lab/" + repository + "/releases/latest"
	return []mirror{
		{Name: "GitHub 官方", URL: url},
		{Name: "GitHub 加速（gh-proxy）", URL: "https://gh-proxy.com/" + url},
		{Name: "GitHub 加速（v6 节点）", URL: "https://v6.gh-proxy.org/" + url},
		{Name: "GitHub 加速（ghproxy.net）", URL: "https://ghproxy.net/" + url},
	}
}

func NewServer(version string, staticFiles fs.FS, templateFiles ...fs.FS) http.Handler {
	identity, err := access.New()
	if err != nil {
		panic(err)
	}
	return NewServerWithAuth(version, staticFiles, identity, templateFiles...)
}

// NewServerWithAuth permits tests and embedders to provide an isolated auth
// store instead of reading the current user's albs configuration.
func NewServerWithAuth(version string, staticFiles fs.FS, identity *access.Manager, templateFiles ...fs.FS) http.Handler {
	assets, err := fs.Sub(staticFiles, "dist")
	if err != nil {
		panic(err)
	}
	aiManager, err := ai.New()
	if err != nil {
		panic(err)
	}
	s := &server{version: version, assets: assets, static: http.FileServer(http.FS(assets)), checker: system.NewChecker(), plugins: setupplugin.NewRegistry(), auth: identity, ai: aiManager, development: map[string]developmentProcess{}, stopping: map[string]bool{}, directoryRoots: managedDirectoryRoots()}
	if len(templateFiles) > 0 {
		templates, err := fs.Sub(templateFiles[0], "templates")
		if err != nil {
			panic(err)
		}
		s.creator = project.NewCreator(templates)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/api/v1/auth/status", s.authStatusHandler)
	mux.HandleFunc("/api/v1/auth/setup", s.authSetupHandler)
	mux.HandleFunc("/api/v1/auth/login", s.authLoginHandler)
	mux.HandleFunc("/api/v1/auth/logout", s.authLogoutHandler)
	mux.HandleFunc("/api/v1/app", s.app)
	mux.HandleFunc("/api/v1/goals", s.listGoals)
	mux.HandleFunc("/api/v1/tasks", s.tasksHandler)
	mux.HandleFunc("/api/v1/checks", s.checksHandler)
	mux.HandleFunc("/api/v1/projects", s.projectsHandler)
	mux.HandleFunc("/api/v1/releases", s.releasesHandler)
	mux.HandleFunc("/api/v1/update", s.updateHandler)
	mux.HandleFunc("/api/v1/update/download", s.downloadUpdateHandler)
	mux.HandleFunc("/api/v1/update/apply", s.applyUpdateHandler)
	mux.HandleFunc("/api/v1/update/load", s.loadUpdateHandler)
	mux.HandleFunc("/api/v1/ai/providers", s.aiProvidersHandler)
	mux.HandleFunc("/api/v1/ai/chat", s.aiChatHandler)
	mux.HandleFunc("/api/v1/system/ssh", s.sshHandler)
	mux.HandleFunc("/api/v1/directories", s.directoryHandler)
	mux.HandleFunc("/api/v1/catalog", s.catalogHandler)
	mux.HandleFunc("/api/v1/catalog/versions", s.catalogVersionsHandler)
	mux.HandleFunc("/api/v1/catalog/document", s.catalogDocumentHandler)
	mux.HandleFunc("/api/v1/catalog/package-config", s.catalogPackageConfigHandler)
	mux.HandleFunc("/api/v1/setup/plugins", s.setupPluginsHandler)
	mux.HandleFunc("/api/v1/setup/plugins/", s.setupPluginActionHandler)
	mux.HandleFunc("/api/v1/robot", s.robotHandler)
	mux.HandleFunc("/api/v1/robot/validate", s.robotValidateHandler)
	mux.HandleFunc("/api/v1/robot/console", s.robotConsoleHandler)
	mux.HandleFunc("/api/v1/robot/pm2-logs", s.robotPM2LogsHandler)
	mux.HandleFunc("/api/v1/robot/runtime", s.robotRuntimeHandler)
	mux.HandleFunc("/api/v1/robot/runtime/preflight", s.robotRuntimePreflightHandler)
	mux.HandleFunc("/api/v1/robot/tasks", s.robotTasksHandler)
	mux.HandleFunc("/api/v1/robot/packages", s.robotPackagesHandler)
	mux.HandleFunc("/api/v1/robot/package-versions", s.robotPackageVersionsHandler)
	mux.HandleFunc("/api/v1/robot/package-readme", s.robotPackageReadmeHandler)
	mux.HandleFunc("/api/v1/robot/webviews", s.robotWebViewsHandler)
	mux.HandleFunc("/api/v1/robot/webview/", s.robotWebViewHandler)
	mux.HandleFunc("/api/v1/robot/package-config", s.robotPackageConfigHandler)
	mux.HandleFunc("/api/v1/robot/login", s.robotLoginHandler)
	mux.HandleFunc("/api/v1/robot/manifest", s.robotManifestHandler)
	mux.HandleFunc("/api/v1/robot/git-init", s.robotGitInitHandler)
	mux.HandleFunc("/api/v1/robot/git", s.robotGitHandler)
	mux.HandleFunc("/api/v1/robot/git-clone", s.robotGitCloneHandler)
	mux.HandleFunc("/api/v1/robot/git-clone/check", s.robotGitCloneCheckHandler)
	mux.HandleFunc("/api/v1/publish/npm/status", s.npmPublishStatusHandler)
	mux.HandleFunc("/api/v1/publish/npm/pack", s.npmPackPreviewHandler)
	mux.HandleFunc("/api/v1/publish/git/status", s.gitPublishStatusHandler)
	mux.Handle("/", s.spa())
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery(), s.ginHeaders(), s.ginAccess(), s.ginRequestLog())
	// The Gin engine owns every request. Existing handlers remain standard
	// net/http functions, preserving their API contracts during migration.
	router.Any("/*path", gin.WrapH(mux))
	return router
}

const authCookieName = "albs_session"

func (s *server) authToken(r *http.Request) string {
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (s *server) authStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	status, err := s.auth.Status(s.authToken(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *server) authSetupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Account      string `json:"account"`
		Password     string `json:"password"`
		Confirmation string `json:"confirmation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请填写账户、密码和确认密码。")
		return
	}
	token, err := s.auth.Enable(input.Account, input.Password, input.Confirmation)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.setAuthCookie(w, token)
	writeJSON(w, http.StatusCreated, map[string]any{"enabled": true, "account": strings.TrimSpace(input.Account)})
}

func (s *server) authLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Account  string `json:"account"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请填写账户和密码。")
		return
	}
	token, err := s.auth.Login(input.Account, input.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	s.setAuthCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "account": strings.TrimSpace(input.Account)})
}

func (s *server) authLogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: authCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func (s *server) setAuthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: authCookieName, Value: token, Path: "/", MaxAge: int((12 * time.Hour).Seconds()), HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func (s *server) npmPublishStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	status, err := s.robots.NPMStatus(r.URL.Query().Get("root"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *server) npmPackPreviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	root, commit := r.URL.Query().Get("root"), r.URL.Query().Get("commit")
	var preview robot.NPMPackPreview
	var err error
	if commit != "" {
		preview, err = s.robots.NPMPackPreviewAtCommit(root, commit)
	} else {
		preview, err = s.robots.NPMPackPreview(root)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *server) gitPublishStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	status, err := robot.GitReleaseStatus(r.URL.Query().Get("root"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *server) catalogHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	groups, err := catalog.Fetch(r.URL.Query().Get("kind"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

func (s *server) catalogVersionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	versions, err := catalog.LoadPackageVersions(r.URL.Query().Get("package"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *server) catalogDocumentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	document, err := catalog.LoadDocument(r.URL.Query().Get("url"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, document)
}

func (s *server) catalogPackageConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	config, err := catalog.LoadPackageConfig(r.URL.Query().Get("url"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, config)
}

// setupPluginsHandler rescans plugin directories on each request. Adding or
// removing a directory is therefore reflected after a normal UI refresh, with
// no process restart and without running third-party code.
func (s *server) setupPluginsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	writeJSON(w, http.StatusOK, s.plugins.All())
}

func (s *server) setupPluginActionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/setup/plugins/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "未找到 Setup 插件操作。")
		return
	}
	if parts[1] == "enabled" {
		var input struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "请选择插件状态。")
			return
		}
		if err := s.plugins.SetEnabled(parts[0], input.Enabled); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": parts[0], "enabled": input.Enabled})
		return
	}
	if parts[1] != "actions" {
		writeError(w, http.StatusNotFound, "未找到 Setup 插件操作。")
		return
	}
	var input struct {
		Action  string            `json:"action"`
		Confirm bool              `json:"confirm"`
		Params  map[string]string `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Action == "" {
		writeError(w, http.StatusBadRequest, "请选择要执行的插件操作。")
		return
	}
	created := operationTask{ID: "setup-" + time.Now().Format("20060102150405.000000000"), Root: "", Action: "setup:" + parts[0] + ":" + input.Action, Status: "running", CreatedAt: time.Now()}
	s.mu.Lock()
	s.operations = append([]operationTask{created}, s.operations...)
	if len(s.operations) > 40 {
		s.operations = s.operations[:40]
	}
	s.mu.Unlock()
	go func() {
		output, err := s.plugins.Run(parts[0], input.Action, input.Params, input.Confirm)
		finished := time.Now()
		s.mu.Lock()
		defer s.mu.Unlock()
		for index := range s.operations {
			if s.operations[index].ID != created.ID {
				continue
			}
			s.operations[index].Status = "completed"
			s.operations[index].Output = output
			s.operations[index].FinishedAt = &finished
			if err != nil {
				s.operations[index].Status = "failed"
				s.operations[index].Error = err.Error()
			}
			break
		}
	}()
	writeJSON(w, http.StatusAccepted, created)
}

func (s *server) sshHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		keys, err := system.SSHKeys()
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
	case http.MethodPost:
		key, err := system.GenerateSSHKey()
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, key)
	default:
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
	}
}

func (s *server) directoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	roots := s.currentDirectoryRoots()
	path, err := managedDirectory(r.URL.Query().Get("path"), roots)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsPermission(err) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "没有读取此位置的权限。请在系统设置中为 albs 授予“文件与文件夹”或“完全磁盘访问”权限，然后重试。", "permission": true})
			return
		}
		writeError(w, http.StatusBadRequest, "无法读取该目录")
		return
	}
	type directory struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	showHidden := r.URL.Query().Get("hidden") == "true"
	directories := make([]directory, 0)
	for _, entry := range entries {
		if entry.IsDir() && (showHidden || !strings.HasPrefix(entry.Name(), ".")) {
			directories = append(directories, directory{Name: entry.Name(), Path: filepath.Join(path, entry.Name())})
		}
	}
	sort.Slice(directories, func(i, j int) bool { return directories[i].Name < directories[j].Name })
	parent := ""
	for _, root := range roots {
		if filepath.Clean(path) != filepath.Clean(root) {
			if next := filepath.Dir(path); isWithinRoot(next, root) {
				parent = next
			}
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "parent": parent, "roots": roots, "locations": directoryLocations(roots), "directories": directories})
}

func managedDirectoryRoots() []string {
	value := os.Getenv("ALEMONJS_SETUP_ROOTS")
	if value == "" {
		roots := []string{}
		if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots, home)
		}
		if runtime.GOOS != "windows" {
			roots = appendDirectoryRoot(roots, string(filepath.Separator))
		}
		for _, mount := range mountedDirectoryRoots() {
			roots = appendDirectoryRoot(roots, mount)
		}
		if len(roots) == 0 {
			return []string{"/"}
		}
		return roots
	}
	roots := []string{}
	for _, item := range filepath.SplitList(value) {
		if path, err := filepath.Abs(item); err == nil {
			roots = append(roots, filepath.Clean(path))
		}
	}
	return roots
}

func appendDirectoryRoot(roots []string, path string) []string {
	path = filepath.Clean(path)
	for _, root := range roots {
		if root == path {
			return roots
		}
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return append(roots, path)
	}
	return roots
}

func mountedDirectoryRoots() []string {
	var base []string
	switch runtime.GOOS {
	case "darwin":
		base = []string{"/Volumes"}
	case "linux":
		if home, err := os.UserHomeDir(); err == nil {
			base = []string{"/media/" + filepath.Base(home), "/run/media/" + filepath.Base(home), "/mnt"}
		}
	case "windows":
		for drive := 'A'; drive <= 'Z'; drive++ {
			base = append(base, string(drive)+":\\")
		}
	}
	roots := []string{}
	for _, parent := range base {
		if runtime.GOOS == "windows" {
			roots = appendDirectoryRoot(roots, parent)
			continue
		}
		entries, err := os.ReadDir(parent)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				roots = appendDirectoryRoot(roots, filepath.Join(parent, entry.Name()))
			}
		}
	}
	return roots
}

func directoryLocations(roots []string) []map[string]string {
	home, _ := os.UserHomeDir()
	items := make([]map[string]string, 0, len(roots))
	for _, root := range roots {
		name, kind := filepath.Base(root), "disk"
		if filepath.Clean(root) == filepath.Clean(home) {
			name, kind = "主目录", "home"
		}
		if filepath.Clean(root) == string(filepath.Separator) {
			name = "系统磁盘"
		}
		if runtime.GOOS == "darwin" && strings.HasPrefix(filepath.Clean(root), "/Volumes/") {
			kind = "volume"
		}
		if runtime.GOOS == "linux" && (strings.HasPrefix(root, "/media/") || strings.HasPrefix(root, "/run/media/")) {
			kind = "volume"
		}
		if runtime.GOOS == "windows" {
			kind = "volume"
		}
		items = append(items, map[string]string{"name": name, "path": root, "kind": kind})
	}
	return items
}

func (s *server) currentDirectoryRoots() []string {
	if os.Getenv("ALEMONJS_SETUP_ROOTS") != "" {
		return s.directoryRoots
	}
	return managedDirectoryRoots()
}

func (s *server) managedDirectory(requested string) (string, error) {
	return managedDirectory(requested, s.currentDirectoryRoots())
}

func managedDirectory(requested string, roots []string) (string, error) {
	path := requested
	if path == "" {
		path = roots[0]
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		if isWithinRoot(absolute, root) {
			return absolute, nil
		}
	}
	return "", fmt.Errorf("目录不在允许的管理范围内")
}

func isWithinRoot(path, root string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (s *server) releasesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	items, err := releases.List(r.URL.Query().Get("app"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *server) updateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	update, err := releases.SetupUpdate(s.version)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if update.Available && update.PlatformMatched {
		_, update.DownloadReady, err = system.CachedUpdate(update.AssetName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, update)
}

func (s *server) aiProvidersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		items, err := s.ai.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct{ Provider, BaseURL, Model, APIKey string }
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "AI 配置无法识别。")
		return
	}
	if err := s.ai.Save(input.Provider, input.BaseURL, input.Model, input.APIKey); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": "AI 配置已仅保存到本机。"})
}

func (s *server) aiChatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Provider string              `json:"provider"`
		Messages []map[string]string `json:"messages"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil || len(input.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "请填写要发送的消息。")
		return
	}
	if len(input.Messages) > 30 {
		writeError(w, http.StatusBadRequest, "一次对话最多保留 30 条消息。")
		return
	}
	answer, err := s.ai.Chat(input.Provider, input.Messages)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"answer": answer})
}

// downloadUpdateHandler downloads the exact asset selected by the server's
// current-version/platform check. The browser never supplies an arbitrary URL.
func (s *server) downloadUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	update, err := releases.SetupUpdate(s.version)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if !update.Available {
		writeError(w, http.StatusBadRequest, "当前已经是最新版本。")
		return
	}
	if !update.PlatformMatched {
		writeError(w, http.StatusBadRequest, "未找到当前系统对应的更新包，请使用手动更新。")
		return
	}
	if _, err := system.DownloadUpdate(update.DownloadURL, update.AssetName); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": "更新包已下载完成。确认后将立即更新并重启应用。", "assetName": update.AssetName})
}

func (s *server) applyUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Confirm bool `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || !input.Confirm {
		writeError(w, http.StatusBadRequest, "请确认立即更新并重启应用。")
		return
	}
	update, err := releases.SetupUpdate(s.version)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if !update.Available || !update.PlatformMatched {
		writeError(w, http.StatusBadRequest, "当前没有可立即安装的更新。")
		return
	}
	path, ready, err := system.CachedUpdate(update.AssetName)
	if err != nil || !ready {
		writeError(w, http.StatusBadRequest, "更新包尚未下载完成，请先下载。")
		return
	}
	result, err := system.ReplaceExecutableFile(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := system.ScheduleRestart(); err != nil {
		writeError(w, http.StatusInternalServerError, "更新已完成，但无法自动重启："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": result + " 正在重启应用。"})
	go func() {
		time.Sleep(350 * time.Millisecond)
		os.Exit(0)
	}()
}

func (s *server) loadUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 200<<20)
	if err := r.ParseMultipartForm(200 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "更新包无效或超过 200 MB。")
		return
	}
	if r.FormValue("confirm") != "true" {
		writeError(w, http.StatusBadRequest, "请确认载入更新包。")
		return
	}
	file, header, err := r.FormFile("package")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择更新包。")
		return
	}
	defer file.Close()
	lower := strings.ToLower(header.Filename)
	if !strings.HasSuffix(lower, ".zip") && !strings.HasSuffix(lower, ".tar.gz") && !strings.HasSuffix(lower, ".tgz") {
		writeError(w, http.StatusBadRequest, "更新包应为 GitHub Release 下载的 zip 或 tar.gz 文件。")
		return
	}
	directory, err := os.MkdirTemp("", "albs-upload-")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, filepath.Base(header.Filename))
	output, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, copyErr := io.Copy(output, file)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		writeError(w, http.StatusBadRequest, "无法读取更新包。")
		return
	}
	result, err := system.ReplaceExecutableFile(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": result})
}

func (s *server) robotHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Root    string `json:"root"`
		File    string `json:"file"`
		Content string `json:"content"`
		Action  string `json:"action"`
		Message string `json:"message"`
		Package string `json:"package"`
		Version string `json:"version"`
		Tag     string `json:"tag"`
		Token   string `json:"token"`
		Confirm string `json:"confirm"`
	}
	if r.Method == http.MethodGet {
		input.Root = r.URL.Query().Get("root")
		input.File = r.URL.Query().Get("file")
	} else if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别。")
		return
	}
	if r.Method == http.MethodPost {
		log.Printf("[ROBOT 同步] 开始 action=%s root=%q", input.Action, input.Root)
	}
	var result robot.Result
	var err error
	switch r.Method {
	case http.MethodGet:
		result, err = s.robots.Read(input.Root, input.File)
	case http.MethodPut:
		result, err = s.robots.Write(input.Root, input.File, input.Content)
	case http.MethodPost:
		result, err = s.robots.Run(input.Root, input.Action, input.Message, input.Package, input.Version, input.Tag, input.Token, input.Confirm == "true")
	default:
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	if err != nil {
		if r.Method == http.MethodPost {
			log.Printf("[ROBOT 同步] 失败 action=%s root=%q error=%s", input.Action, input.Root, err)
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.Method == http.MethodPost {
		log.Printf("[ROBOT 同步] 完成 action=%s root=%q output=%dB", input.Action, input.Root, len(result.Output))
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) robotConsoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	root := r.URL.Query().Get("root")
	result, err := s.robots.Console(root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	output, status := s.operationOutput(root, "app")
	mode := "前台运行"
	if output == "" {
		output, status = s.operationOutput(root, "dev")
		mode = "开发模式"
	}
	if output != "" {
		label := "最近一次" + mode + "输出"
		if status == "running" {
			label = mode + "实时输出"
		}
		result.Output += "\n\n$ " + label + "\n" + output
	} else {
		result.Output += "\n\n$ 运行终端\n当前没有正在运行的前台或开发进程。"
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) robotPM2LogsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		if _, err := fmt.Sscanf(raw, "%d", &page); err != nil || page < 1 {
			writeError(w, http.StatusBadRequest, "日志页码无效。")
			return
		}
	}
	result, err := s.robots.PM2Logs(r.URL.Query().Get("root"), page)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) robotRuntimeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	overview, err := s.robots.RuntimeOverview(r.URL.Query().Get("root"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (s *server) robotRuntimePreflightHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	preflight, err := s.robots.RuntimePreflight(r.URL.Query().Get("root"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preflight)
}

func (s *server) robotValidateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	result, err := s.robots.Validate(r.URL.Query().Get("root"))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "path": result.Path})
}

func (s *server) robotTasksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		id := r.URL.Query().Get("id")
		s.mu.RLock()
		defer s.mu.RUnlock()
		if id == "" {
			operations := s.operations
			if operations == nil {
				operations = []operationTask{}
			}
			writeJSON(w, http.StatusOK, operations)
			return
		}
		for _, item := range s.operations {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, "操作任务不存在或已过期。")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Root    string `json:"root"`
		Action  string `json:"action"`
		Message string `json:"message"`
		Package string `json:"package"`
		Version string `json:"version"`
		Tag     string `json:"tag"`
		Token   string `json:"token"`
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别。")
		return
	}
	if input.Root == "" || input.Action == "" {
		writeError(w, http.StatusBadRequest, "请选择机器人目录和操作。")
		return
	}
	if _, err := s.robots.Validate(input.Root); err != nil {
		writeError(w, http.StatusBadRequest, "当前机器人目录不可用："+err.Error()+"。请在左侧移除后重新选择目录。")
		return
	}
	created := operationTask{ID: "robot-" + time.Now().Format("20060102150405.000000000"), Root: input.Root, Action: input.Action, Status: "running", CreatedAt: time.Now()}
	if input.Action == "dev-stop" || input.Action == "app-stop" {
		mode := map[string]string{"dev-stop": "开发模式", "app-stop": "前台运行"}[input.Action]
		if err := s.stopDevelopment(input.Root); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		finished := time.Now()
		created.Status = "completed"
		created.Output = "已请求停止" + mode + "；等待进程退出。"
		created.FinishedAt = &finished
		s.addOperation(created)
		writeJSON(w, http.StatusAccepted, created)
		return
	}
	if input.Action == "dev" || input.Action == "app" {
		missing, dependencyErr := s.robots.RuntimeDependencies(input.Root)
		if dependencyErr != nil {
			writeError(w, http.StatusBadRequest, dependencyErr.Error())
			return
		}
		if len(missing) > 0 {
			writeError(w, http.StatusBadRequest, "项目依赖不完整："+strings.Join(missing, "、")+"。请先执行“重载依赖”后再运行")
			return
		}
		if s.developmentRunning(input.Root) {
			writeError(w, http.StatusConflict, "当前目录的开发模式正在运行；请先停止后再启动。")
			return
		}
		command, err := s.robots.DevelopmentCommand(input.Root)
		if input.Action == "app" {
			command, err = s.robots.ForegroundCommand(input.Root)
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		stdout, err := command.StdoutPipe()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "无法连接运行输出："+err.Error())
			return
		}
		stderr, err := command.StderrPipe()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "无法连接运行错误输出："+err.Error())
			return
		}
		if err := command.Start(); err != nil {
			writeError(w, http.StatusBadRequest, "运行启动失败："+err.Error())
			return
		}
		if !s.registerDevelopment(input.Root, created.ID, command) {
			_ = command.Process.Kill()
			writeError(w, http.StatusConflict, "当前目录的开发模式正在运行；请先停止后再启动。")
			return
		}
		created.Output = map[bool]string{true: "开发模式已启动，正在等待进程输出…\n", false: "前台运行已启动，正在等待进程输出…\n"}[input.Action == "dev"]
		s.addOperation(created)
		log.Printf("[ROBOT %s] 开始 action=%s root=%q", created.ID, input.Action, created.Root)
		go s.watchDevelopmentTask(created.ID, input.Root, command, stdout, stderr)
		writeJSON(w, http.StatusAccepted, created)
		return
	}
	log.Printf("[ROBOT %s] 开始 action=%s root=%q", created.ID, created.Action, created.Root)
	s.addOperation(created)
	go func() {
		result, err := s.robots.Run(input.Root, input.Action, input.Message, input.Package, input.Version, input.Tag, input.Token, input.Confirm == "true")
		finished := time.Now()
		s.mu.Lock()
		defer s.mu.Unlock()
		for index := range s.operations {
			if s.operations[index].ID == created.ID {
				s.operations[index].Status = "completed"
				s.operations[index].Output = result.Output
				s.operations[index].FinishedAt = &finished
				if err != nil {
					s.operations[index].Status = "failed"
					s.operations[index].Error = err.Error()
				}
				break
			}
		}
		if err != nil {
			log.Printf("[ROBOT %s] 失败 action=%s root=%q error=%s", created.ID, created.Action, created.Root, err)
			return
		}
		log.Printf("[ROBOT %s] 完成 action=%s root=%q output=%dB", created.ID, created.Action, created.Root, len(result.Output))
	}()
	writeJSON(w, http.StatusAccepted, created)
}

func (s *server) addOperation(created operationTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.operations = append([]operationTask{created}, s.operations...)
	if len(s.operations) > 40 {
		s.operations = s.operations[:40]
	}
}

func (s *server) operationOutput(root, action string) (string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.operations {
		if item.Root == root && item.Action == action {
			return item.Output, item.Status
		}
	}
	return "", ""
}

func (s *server) developmentRunning(root string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, running := s.development[root]
	return running
}

func (s *server) registerDevelopment(root, taskID string, command *exec.Cmd) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, running := s.development[root]; running {
		return false
	}
	s.development[root] = developmentProcess{TaskID: taskID, Command: command}
	return true
}

func (s *server) stopDevelopment(root string) error {
	s.mu.Lock()
	process, running := s.development[root]
	if !running {
		s.mu.Unlock()
		return fmt.Errorf("当前目录没有正在运行的前台或开发进程")
	}
	s.stopping[root] = true
	s.mu.Unlock()
	s.appendOperationOutput(process.TaskID, "正在停止托管进程…\n")
	if err := process.Command.Process.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("无法停止开发进程：%w", err)
	}
	// Some package managers do not pass Interrupt through immediately. Give the
	// command a short graceful window, then ensure the managed parent exits.
	time.AfterFunc(5*time.Second, func() {
		s.mu.RLock()
		current, active := s.development[root]
		stopping := s.stopping[root]
		s.mu.RUnlock()
		if active && stopping && current.TaskID == process.TaskID {
			_ = current.Command.Process.Kill()
		}
	})
	return nil
}

func (s *server) appendOperationOutput(id, output string) {
	if output == "" {
		return
	}
	const maxOutput = 256 * 1024
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.operations {
		if s.operations[index].ID != id {
			continue
		}
		s.operations[index].Output += output
		if len(s.operations[index].Output) > maxOutput {
			s.operations[index].Output = "…前面的输出已省略…\n" + s.operations[index].Output[len(s.operations[index].Output)-maxOutput:]
		}
		return
	}
}

func (s *server) watchDevelopmentTask(id, root string, command *exec.Cmd, stdout, stderr io.Reader) {
	var readers sync.WaitGroup
	read := func(stream io.Reader) {
		defer readers.Done()
		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			s.appendOperationOutput(id, scanner.Text()+"\n")
		}
		if err := scanner.Err(); err != nil {
			s.appendOperationOutput(id, "读取进程输出失败："+err.Error()+"\n")
		}
	}
	readers.Add(2)
	go read(stdout)
	go read(stderr)
	err := command.Wait()
	readers.Wait()

	finished := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	stopped := s.stopping[root]
	if current, active := s.development[root]; active && current.TaskID == id {
		delete(s.development, root)
		delete(s.stopping, root)
	}
	for index := range s.operations {
		if s.operations[index].ID != id {
			continue
		}
		s.operations[index].FinishedAt = &finished
		if err != nil && !stopped {
			s.operations[index].Status = "failed"
			s.operations[index].Error = "开发进程已退出：" + err.Error()
			log.Printf("[ROBOT %s] 开发进程退出 error=%s", id, err)
		} else if stopped {
			s.operations[index].Status = "completed"
			s.operations[index].Output += "开发进程已停止。\n"
			log.Printf("[ROBOT %s] 开发进程已停止", id)
		} else {
			s.operations[index].Status = "completed"
			s.operations[index].Output += "开发进程已正常退出。\n"
			log.Printf("[ROBOT %s] 开发进程正常退出", id)
		}
		return
	}
}

func (s *server) robotPackagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	items, err := s.robots.LocalPackages(r.URL.Query().Get("root"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) robotPackageVersionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	versions, err := s.robots.LocalPackageVersions(r.URL.Query().Get("root"), r.URL.Query().Get("package"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *server) robotPackageReadmeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	result, err := s.robots.LocalPackageReadme(r.URL.Query().Get("root"), r.URL.Query().Get("package"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) robotWebViewsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	items, err := s.robots.WebViews(r.URL.Query().Get("root"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *server) robotWebViewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	const prefix = "/api/v1/robot/webview/"
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		writeError(w, http.StatusBadRequest, "缺少插件 Web 页面标识。")
		return
	}
	// Treat the entry URL as a directory. Vite commonly emits ./assets/...;
	// without this redirect a caller omitting the final slash resolves those
	// files one level above the registered WebView id.
	if len(parts) == 2 && !strings.HasSuffix(r.URL.Path, "/") {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusTemporaryRedirect)
		return
	}
	rootBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "机器人目录标识无效。")
		return
	}
	requestPath := ""
	if len(parts) == 3 {
		requestPath = parts[2]
	}
	if strings.HasPrefix(requestPath, "api/") {
		s.proxyRobotWebViewAPI(w, r, string(rootBytes), parts[1], strings.TrimPrefix(requestPath, "api/"))
		return
	}
	file, err := s.robots.WebViewFile(string(rootBytes), parts[1], requestPath)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	// WebViews are intentionally opened through the other loopback hostname
	// (localhost <-> 127.0.0.1). That keeps their localStorage and cookies in a
	// separate browser origin from the management UI. Plugin actions can only
	// use the narrowly scoped WebView API proxy below.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// A robot plugin UI may talk only to its registered local bot API proxy.
	// Do not permit the parent management origin in connect-src.
	w.Header().Set("Content-Security-Policy", "default-src 'self' data: blob:; connect-src 'self' ws: wss:; img-src 'self' data: blob: https: http:; style-src 'self' 'unsafe-inline'; frame-ancestors http://localhost:* http://127.0.0.1:*; base-uri 'none'")
	// WebView assets are static and path-guarded. Keep this header for modules
	// and assets that a plugin may load from a sandboxed or alternate browser
	// origin; it does not grant a setup command bridge.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// Vite's default production output commonly uses /assets/... absolute URLs.
	// Inside a plugin mount that would otherwise point at setup's own assets.
	// Keep ordinary external URLs untouched and only make local bundle paths
	// relative to this plugin's registered WebView route.
	if filepath.Ext(file) == ".html" {
		data, readErr := os.ReadFile(file)
		if readErr != nil {
			writeError(w, http.StatusNotFound, "插件 Web 页面不存在。")
			return
		}
		content := strings.NewReplacer(
			`src="/assets/`, `src="assets/`,
			`href="/assets/`, `href="assets/`,
			`src='/assets/`, `src='assets/`,
			`href='/assets/`, `href='assets/`,
		).Replace(string(data))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, content)
		return
	}
	http.ServeFile(w, r, file)
}

// proxyRobotWebViewAPI connects a WebView's relative ./api/* requests to the
// selected robot application. The destination is never supplied by the
// browser: it is derived from the selected root's configured local app port.
func (s *server) proxyRobotWebViewAPI(w http.ResponseWriter, r *http.Request, root, id, requestPath string) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "插件 API 不支持此请求方式。")
		return
	}
	target, err := s.robots.WebViewAPIURL(root, id, requestPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	request, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "插件 API 请求无效。")
		return
	}
	for _, header := range []string{"Accept", "Content-Type"} {
		if value := r.Header.Get(header); value != "" {
			request.Header.Set(header, value)
		}
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		writeError(w, http.StatusBadGateway, "机器人应用尚未启动或无法连接。请在“运行”中启动开发模式后重试。")
		return
	}
	defer response.Body.Close()
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (s *server) robotManifestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		manifest, err := s.robots.PackageManifest(r.URL.Query().Get("root"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, manifest)
		return
	}
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Root string `json:"root"`
		robot.PackageManifest
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别。")
		return
	}
	result, err := s.robots.SavePackageManifest(input.Root, input.PackageManifest)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) robotPackageConfigHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Root    string            `json:"root"`
		Package string            `json:"package"`
		Values  map[string]string `json:"values"`
	}
	if r.Method == http.MethodGet {
		input.Root = r.URL.Query().Get("root")
		input.Package = r.URL.Query().Get("package")
	} else if r.Method == http.MethodPut {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "请求内容无法识别。")
			return
		}
	} else {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	if r.Method == http.MethodGet {
		var config robot.PackageConfig
		var err error
		if input.Package == "" {
			config, err = s.robots.CurrentPackageConfig(input.Root)
		} else {
			config, err = s.robots.PackageConfig(input.Root, input.Package)
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, config)
		return
	}
	var result robot.Result
	var err error
	if input.Package == "" {
		result, err = s.robots.SaveCurrentPackageConfig(input.Root, input.Values)
	} else {
		result, err = s.robots.SavePackageConfig(input.Root, input.Package, input.Values)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) robotLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Root    string `json:"root"`
		Login   string `json:"login"`
		Package string `json:"package"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别。")
		return
	}
	result, err := s.robots.SaveLogin(input.Root, input.Login, input.Package)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) robotGitInitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Root string `json:"root"`
		robot.GitInitConfig
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别。")
		return
	}
	result, err := robot.InitializeGit(input.Root, input.GitInitConfig)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) robotGitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		status, err := robot.GitWorkspace(r.URL.Query().Get("root"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Root    string `json:"root"`
		Action  string `json:"action"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Git 操作内容无法识别。")
		return
	}
	result, err := robot.GitWorkspaceAction(input.Root, input.Action, input.Message)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) robotGitCloneHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Destination string `json:"destination"`
		Repository  string `json:"repository"`
		Branch      string `json:"branch"`
		Name        string `json:"name"`
		Mirror      string `json:"mirror"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Git 信息无法识别。")
		return
	}
	destination, err := s.managedDirectory(input.Destination)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	result, err := robot.CloneRepository(destination, input.Repository, input.Branch, input.Name, input.Mirror)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *server) robotGitCloneCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	destination, err := s.managedDirectory(r.URL.Query().Get("destination"))
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	target, err := robot.CloneDestination(destination, r.URL.Query().Get("repository"), r.URL.Query().Get("name"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, target)
}

func (s *server) projectsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	if s.creator == nil {
		writeError(w, http.StatusServiceUnavailable, "当前运行包未包含项目模板，无法创建项目。")
		return
	}
	var input project.Config
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "创建参数无法识别，请检查项目名称和保存位置。")
		return
	}
	created, err := s.creator.Create(input)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "result": created})
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *server) spa() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if _, err := fs.Stat(s.assets, r.URL.Path[1:]); err == nil {
				s.static.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/"
		s.static.ServeHTTP(w, r)
	})
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": s.version})
}

func (s *server) app(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"name": "alemonjs-setup", "version": s.version})
}

func (s *server) listGoals(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, goals)
}

func (s *server) listTasks(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, http.StatusOK, s.tasks)
}

func (s *server) tasksHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listTasks(w, r)
	case http.MethodPost:
		s.createTask(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
	}
}

func (s *server) checksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		GoalID  string `json:"goalId"`
		Variant string `json:"variant"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别，请重新选择操作目标。")
		return
	}
	if _, ok := findGoal(input.GoalID); !ok {
		writeError(w, http.StatusBadRequest, "所选操作目标不存在，请返回首页重新选择。")
		return
	}
	if input.Variant != "" {
		valid := input.GoalID == "web" && (input.Variant == "clean" || input.Variant == "docker") || input.GoalID == "build" && (input.Variant == "npm" || input.Variant == "git")
		if !valid {
			writeError(w, http.StatusBadRequest, "构建或部署方式无效，请重新选择。")
			return
		}
	}
	writeJSON(w, http.StatusOK, s.checker.CheckGoal(input.GoalID, input.Variant))
}

func (s *server) createTask(w http.ResponseWriter, r *http.Request) {
	var input struct {
		GoalID string `json:"goalId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别，请重新选择操作目标。")
		return
	}

	selected, ok := findGoal(input.GoalID)
	if !ok {
		writeError(w, http.StatusBadRequest, "所选操作目标不存在，请返回首页重新选择。")
		return
	}

	created := task{
		ID:        input.GoalID + "-" + time.Now().Format("20060102150405.000000000"),
		GoalID:    input.GoalID,
		Status:    "ready",
		Title:     selected.Title,
		CreatedAt: time.Now(),
	}
	s.mu.Lock()
	s.tasks = append([]task{created}, s.tasks...)
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, created)
}

func findGoal(id string) (goal, bool) {
	for _, item := range goals {
		if item.ID == id {
			return item, true
		}
	}
	return goal{}, false
}

func (s *server) ginHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		// Registered robot WebViews are embedded through the other loopback
		// hostname, so X-Frame-Options cannot be SAMEORIGIN. Their own CSP
		// allows only localhost/127.0.0.1 parents. Everything else is denied.
		if !strings.HasPrefix(c.Request.URL.Path, "/api/v1/robot/webview/") {
			c.Header("X-Frame-Options", "DENY")
		}
		c.Next()
	}
}

// ginAccess protects every management API after local identity verification is
// enabled. Static files stay available so a browser can render the login view.
func (s *server) ginAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/api/v1/") || path == "/api/v1/auth/status" || path == "/api/v1/auth/setup" || path == "/api/v1/auth/login" || path == "/api/v1/auth/logout" || strings.HasPrefix(path, "/api/v1/robot/webview/") {
			c.Next()
			return
		}
		status, err := s.auth.Status("")
		if err != nil {
			writeError(c.Writer, http.StatusInternalServerError, err.Error())
			c.Abort()
			return
		}
		if !status.Enabled || s.auth.Authenticate(s.authToken(c.Request)) {
			c.Next()
			return
		}
		writeError(c.Writer, http.StatusUnauthorized, "请先登录身份认证账户。")
		c.Abort()
	}
}

func (s *server) ginRequestLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := s.requestID.Add(1)
		started := time.Now()
		c.Set("requestID", id)
		log.Printf("[GIN %06d] 开始 %s %s", id, c.Request.Method, c.Request.URL.Path)
		c.Next()
		status := c.Writer.Status()
		label := "完成"
		if status >= http.StatusBadRequest {
			label = "失败"
		}
		log.Printf("[GIN %06d] %s status=%d duration=%s response=%dB", id, label, status, time.Since(started).Round(time.Millisecond), c.Writer.Size())
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
