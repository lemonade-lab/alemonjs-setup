// Package web provides the embedded setup guide and its HTTP API.
package web

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

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
	assets, err := fs.Sub(staticFiles, "dist")
	if err != nil {
		panic(err)
	}
	s := &server{version: version, assets: assets, static: http.FileServer(http.FS(assets)), checker: system.NewChecker(), plugins: setupplugin.NewRegistry(), development: map[string]developmentProcess{}, stopping: map[string]bool{}, directoryRoots: managedDirectoryRoots()}
	if len(templateFiles) > 0 {
		templates, err := fs.Sub(templateFiles[0], "templates")
		if err != nil {
			panic(err)
		}
		s.creator = project.NewCreator(templates)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/api/v1/app", s.app)
	mux.HandleFunc("/api/v1/goals", s.listGoals)
	mux.HandleFunc("/api/v1/tasks", s.tasksHandler)
	mux.HandleFunc("/api/v1/checks", s.checksHandler)
	mux.HandleFunc("/api/v1/projects", s.projectsHandler)
	mux.HandleFunc("/api/v1/releases", s.releasesHandler)
	mux.HandleFunc("/api/v1/update", s.updateHandler)
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
	mux.HandleFunc("/api/v1/robot/tasks", s.robotTasksHandler)
	mux.HandleFunc("/api/v1/robot/packages", s.robotPackagesHandler)
	mux.HandleFunc("/api/v1/robot/package-config", s.robotPackageConfigHandler)
	mux.HandleFunc("/api/v1/robot/manifest", s.robotManifestHandler)
	mux.HandleFunc("/api/v1/robot/git-init", s.robotGitInitHandler)
	mux.HandleFunc("/api/v1/publish/npm/status", s.npmPublishStatusHandler)
	mux.HandleFunc("/api/v1/publish/npm/pack", s.npmPackPreviewHandler)
	mux.HandleFunc("/api/v1/publish/git/status", s.gitPublishStatusHandler)
	mux.Handle("/", s.spa())
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery(), s.ginHeaders(), s.ginRequestLog())
	// The Gin engine owns every request. Existing handlers remain standard
	// net/http functions, preserving their API contracts during migration.
	router.Any("/*path", gin.WrapH(mux))
	return router
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

func (s *server) directoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	path, err := s.managedDirectory(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
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
	for _, root := range s.directoryRoots {
		if filepath.Clean(path) != filepath.Clean(root) {
			if next := filepath.Dir(path); isWithinRoot(next, root) {
				parent = next
			}
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "parent": parent, "roots": s.directoryRoots, "directories": directories})
}

func managedDirectoryRoots() []string {
	value := os.Getenv("ALEMONJS_SETUP_ROOTS")
	if value == "" {
		if home, err := os.UserHomeDir(); err == nil {
			return []string{home}
		}
		return []string{"/"}
	}
	roots := []string{}
	for _, item := range filepath.SplitList(value) {
		if path, err := filepath.Abs(item); err == nil {
			roots = append(roots, filepath.Clean(path))
		}
	}
	return roots
}

func (s *server) managedDirectory(requested string) (string, error) {
	path := requested
	if path == "" {
		path = s.directoryRoots[0]
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for _, root := range s.directoryRoots {
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
	writeJSON(w, http.StatusOK, update)
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
	if output, status := s.operationOutput(root, "dev"); output != "" {
		label := "最近一次开发模式输出"
		if status == "running" {
			label = "开发模式实时输出"
		}
		result.Output += "\n\n$ " + label + "\n" + output
	} else {
		result.Output += "\n\n$ 开发模式实时输出\n当前没有正在运行的开发进程。"
	}
	writeJSON(w, http.StatusOK, result)
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
	if input.Action == "dev-stop" {
		if err := s.stopDevelopment(input.Root); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		finished := time.Now()
		created.Status = "completed"
		created.Output = "已请求停止开发模式；等待开发进程退出。"
		created.FinishedAt = &finished
		s.addOperation(created)
		writeJSON(w, http.StatusAccepted, created)
		return
	}
	if input.Action == "dev" {
		if s.developmentRunning(input.Root) {
			writeError(w, http.StatusConflict, "当前目录的开发模式正在运行；请先停止后再启动。")
			return
		}
		command, err := s.robots.DevelopmentCommand(input.Root)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		stdout, err := command.StdoutPipe()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "无法连接开发模式输出："+err.Error())
			return
		}
		stderr, err := command.StderrPipe()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "无法连接开发模式错误输出："+err.Error())
			return
		}
		if err := command.Start(); err != nil {
			writeError(w, http.StatusBadRequest, "开发模式启动失败："+err.Error())
			return
		}
		if !s.registerDevelopment(input.Root, created.ID, command) {
			_ = command.Process.Kill()
			writeError(w, http.StatusConflict, "当前目录的开发模式正在运行；请先停止后再启动。")
			return
		}
		created.Output = "开发模式已启动，正在等待进程输出…\n"
		s.addOperation(created)
		log.Printf("[ROBOT %s] 开始 action=dev root=%q", created.ID, created.Root)
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
		return fmt.Errorf("当前目录没有正在运行的开发模式")
	}
	s.stopping[root] = true
	s.mu.Unlock()
	s.appendOperationOutput(process.TaskID, "正在停止开发进程…\n")
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
		config, err := s.robots.PackageConfig(input.Root, input.Package)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, config)
		return
	}
	result, err := s.robots.SavePackageConfig(input.Root, input.Package, input.Values)
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
		c.Header("X-Frame-Options", "DENY")
		c.Next()
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
