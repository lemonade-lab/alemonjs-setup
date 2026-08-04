// Package web provides the embedded setup guide and its HTTP API.
package web

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"alemonjs-setup/internal/project"
	"alemonjs-setup/internal/robot"
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

type server struct {
	version string
	assets  fs.FS
	static  http.Handler
	checker *system.Checker
	creator *project.Creator
	robots  robot.Manager
	mu      sync.RWMutex
	tasks   []task
}

var goals = []goal{
	{ID: "develop", Title: "开始开发", Description: "创建一个 AlemonJS 项目。", Steps: []string{"环境检查", "项目名称", "开发语言", "代码规范", "版本管理", "本地运行", "包管理器", "图片开发", "样式方案", "开发技能", "确认创建"}},
	{ID: "desktop", Title: "安装桌面版", Description: "下载 AlemonDesk。", Steps: []string{"选择下载镜像", "下载桌面版"}, Mirrors: githubMirrors("alemondesk")},
	{ID: "mobile", Title: "安装手机版", Description: "下载 AlemonApp。", Steps: []string{"选择下载镜像", "下载手机版"}, Mirrors: githubMirrors("alemonapp")},
	{ID: "web", Title: "部署 Web 版", Description: "部署 AlemonGo。", Steps: []string{"选择部署方式", "环境检查", "快速启动"}, DownloadURL: "https://github.com/lemonade-lab/alemongo"},
	{ID: "build", Title: "构建应用", Description: "构建并分发 AlemonJS 应用。", Steps: []string{"选择构建方式", "构建检查", "生成应用"}},
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
	s := &server{version: version, assets: assets, static: http.FileServer(http.FS(assets)), checker: system.NewChecker()}
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
	mux.HandleFunc("/api/v1/robot", s.robotHandler)
	mux.Handle("/", s.spa())
	return s.withHeaders(mux)
}

func (s *server) robotHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Root    string `json:"root"`
		File    string `json:"file"`
		Content string `json:"content"`
		Action  string `json:"action"`
		Message string `json:"message"`
		Package string `json:"package"`
	}
	if r.Method == http.MethodGet {
		input.Root = r.URL.Query().Get("root")
		input.File = r.URL.Query().Get("file")
	} else if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别。")
		return
	}
	var result robot.Result
	var err error
	switch r.Method {
	case http.MethodGet:
		result, err = s.robots.Read(input.Root, input.File)
	case http.MethodPut:
		result, err = s.robots.Write(input.Root, input.File, input.Content)
	case http.MethodPost:
		result, err = s.robots.Run(input.Root, input.Action, input.Message, input.Package)
	default:
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
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

func (s *server) withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
