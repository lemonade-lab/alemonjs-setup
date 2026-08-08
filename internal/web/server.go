// Package web provides the embedded setup guide and its HTTP API.
package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"alemonx/internal/access"
	"alemonx/internal/agent"
	"alemonx/internal/ai"
	"alemonx/internal/catalog"
	"alemonx/internal/project"
	"alemonx/internal/releases"
	"alemonx/internal/robot"
	"alemonx/internal/setupplugin"
	"alemonx/internal/system"
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

type operationTask struct {
	ID         string     `json:"id"`
	Root       string     `json:"root"`
	Path       string     `json:"path,omitempty"`
	Action     string     `json:"action"`
	Status     string     `json:"status"`
	Output     string     `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	Progress   int        `json:"progress"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// robotEvent is pushed to subscribed SSE clients whenever a supervised task
// changes state or a running process emits output. Type is "task" (full task
// snapshot) or "output" (incremental text for a task).
type robotEvent struct {
	Type   string         `json:"type"`
	TaskID string         `json:"taskId,omitempty"`
	Task   *operationTask `json:"task,omitempty"`
	Text   string         `json:"text,omitempty"`
}

// robotEventHub fans events out to open SSE subscribers. A dropped channel is
// pruned on publish so a slow client cannot block the event loop.
type robotEventHub struct {
	mu          sync.Mutex
	subscribers map[chan robotEvent]struct{}
}

func newRobotEventHub() *robotEventHub {
	return &robotEventHub{subscribers: map[chan robotEvent]struct{}{}}
}

func (h *robotEventHub) subscribe() chan robotEvent {
	ch := make(chan robotEvent, 64)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *robotEventHub) unsubscribe(ch chan robotEvent) {
	h.mu.Lock()
	delete(h.subscribers, ch)
	h.mu.Unlock()
}

func (h *robotEventHub) publish(event robotEvent) {
	h.mu.Lock()
	for ch := range h.subscribers {
		select {
		case ch <- event:
		default:
			delete(h.subscribers, ch)
		}
	}
	h.mu.Unlock()
}

type developmentProcess struct {
	TaskID  string
	Command *exec.Cmd
	// PGID is the process-group id created for the command (unix: its own pid).
	// Persisted so an orphaned node can be killed even after alx restarts.
	PGID int
}

// persistedProcess is the on-disk marker for a supervised robot process. It
// survives alx restarts so a stray node that kept the port can be found and
// killed instead of escaping ("端口逃逸").
type persistedProcess struct {
	Root      string    `json:"root"`
	TaskID    string    `json:"taskId"`
	PGID      int       `json:"pgid"`
	Action    string    `json:"action"`
	StartedAt time.Time `json:"startedAt"`
}

func processesFilePath() string {
	if override := os.Getenv("ALEMONX_PROCESS_FILE"); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".alemonx", "processes.json")
}

func loadPersistedProcesses() []persistedProcess {
	path := processesFilePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var items []persistedProcess
	if json.Unmarshal(data, &items) != nil {
		return nil
	}
	return items
}

func savePersistedProcesses(items []persistedProcess) {
	path := processesFilePath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	data, _ := json.Marshal(items)
	_ = os.WriteFile(path, data, 0600)
}

// webViewRuntime is separate from a robot's app/dev process. It hosts
// alemonjs/desktop.js and exchanges plugin messages through stdin/stdout.
type webViewRuntime struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	events  map[string][]json.RawMessage
}

type server struct {
	version           string
	assets            fs.FS
	static            http.Handler
	checker           *system.Checker
	creator           *project.Creator
	robots            robot.Manager
	plugins           *setupplugin.Registry
	pluginWatcher     *setupplugin.Watcher
	auth              *access.Manager
	ai                *ai.Manager
	agentSessions     *agent.SessionStore
	agentTasks        *agent.TaskManager
	taskService       *agent.TaskService
	agentTaskStore    *agent.TaskStore
	goalStore         *agent.GoalStore
	goalSchedulerStop chan struct{}
	goalSchedulerMu   sync.Mutex
	goalRunning       map[string]bool
	opsStore          *agent.OpsStore
	opsOrchestrator   *agent.OpsOrchestrator
	opsPaused         bool
	opsMonitor        *agent.OpsMonitor
	agentConfirms     *agentConfirmManager
	mu                sync.RWMutex
	operations        []operationTask
	development       map[string]developmentProcess
	webviewRuntimes   map[string]*webViewRuntime
	stopping          map[string]bool
	consoleCache      map[string]consoleSnapshot
	// pm2Status lets tests substitute a fake PM2 state. The default read runs a
	// real `npx pm2 jlist` behind a short timeout so a missing pm2 never blocks
	// a local start request for the full package-manager timeout.
	pm2Status      func(string) (robot.PM2Status, error)
	requestID      atomic.Uint64
	directoryRoots []string
	events         *robotEventHub
}

// ServerRuntime owns the HTTP handler and all process-local background loops.
// Existing callers can keep using NewServer; production entrypoints should use
// Runtime.Shutdown so monitors and task state are flushed before exit.
type ServerRuntime struct {
	Handler http.Handler
	server  *server
	once    sync.Once
}

func (r *ServerRuntime) Shutdown(ctx context.Context) error {
	if r == nil || r.server == nil { return nil }
	var shutdownErr error
	r.once.Do(func() {
		s := r.server
		if s.opsMonitor != nil { _ = s.opsMonitor.Stop() }
		s.stopGoalScheduler()
		if s.pluginWatcher != nil { s.pluginWatcher.Stop() }
		if err := s.agentTasks.PauseRunning(); err != nil { shutdownErr = err }
		select { case <-ctx.Done(): if shutdownErr == nil { shutdownErr = ctx.Err() }; default: }
	})
	return shutdownErr
}

// cleanupStaleProcesses runs at server startup. It loads the persisted markers
// for previously supervised processes and kills any that survived an alx
// restart, so an orphaned node cannot hold the app port forever.
func cleanupStaleProcesses() {
	for _, item := range loadPersistedProcesses() {
		if item.PGID > 0 && processGroupAlive(item.PGID) {
			killProcessGroup(item.PGID)
		}
	}
	savePersistedProcesses(nil)
}

// pm2StatusResult carries the outcome of a bounded PM2 status read.
type pm2StatusResult struct {
	status robot.PM2Status
	err    error
}

// consoleSnapshot is a short-lived copy of the terminal's static context
// (pwd, scripts, git status, node version). It barely changes and is far more
// expensive to produce than the in-memory process output, so the terminal
// polling loop reuses it instead of spawning git/node on every tick.
type consoleSnapshot struct {
	output string
	at     time.Time
}

var goals = []goal{
	{ID: "install", Title: "安装机器人", Description: "用推荐默认配置，快速安装一个可以运行的 AlemonJS 机器人。", Steps: []string{"环境检查", "机器人名称与位置", "确认安装"}},
	{ID: "manage", Title: "管理机器人", Description: "管理已有机器人项目的配置、依赖与运行方式。", Steps: []string{"打开机器人管理"}},
	{ID: "develop", Title: "开发机器人", Description: "创建一个可按需配置的 AlemonJS 开发项目。", Steps: []string{"环境检查", "项目名称", "开发语言", "代码规范", "版本管理", "本地运行", "包管理器", "开发能力包", "图片开发", "样式方案", "开发技能", "确认创建"}},
	{ID: "desktop", Title: "安装桌面版", Description: "下载 AlemonDesk。", Steps: []string{"选择下载镜像", "下载桌面版"}, Mirrors: githubMirrors("alemondesk")},
	{ID: "mobile", Title: "安装手机版", Description: "下载 AlemonApp Android 安装包。", Steps: []string{"下载 Android 安装包"}, DownloadURL: "https://download.alemonjs.com/application/alemonapp/app-universal-release.apk"},
	{ID: "web", Title: "部署 Web 版", Description: "部署 alx。", Steps: []string{"选择部署方式", "环境检查", "快速启动"}, Mirrors: githubMirrors("alx")},
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
	return NewServerRuntime(version, staticFiles, templateFiles...).Handler
}

func NewServerRuntime(version string, staticFiles fs.FS, templateFiles ...fs.FS) *ServerRuntime {
	identity, err := access.New()
	if err != nil {
		panic(err)
	}
	return NewServerRuntimeWithAuth(version, staticFiles, identity, templateFiles...)
}

// NewServerWithAuth permits tests and embedders to provide an isolated auth
// store instead of reading the current user's alx configuration.
func NewServerWithAuth(version string, staticFiles fs.FS, identity *access.Manager, templateFiles ...fs.FS) http.Handler {
	return NewServerRuntimeWithAuth(version, staticFiles, identity, templateFiles...).Handler
}

func NewServerRuntimeWithAuth(version string, staticFiles fs.FS, identity *access.Manager, templateFiles ...fs.FS) *ServerRuntime {
	assets, err := fs.Sub(staticFiles, "dist")
	if err != nil {
		panic(err)
	}
	aiManager, err := ai.New()
	if err != nil {
		panic(err)
	}
	sessionStore, err := agent.NewSessionStore()
	if err != nil {
		panic(err)
	}
	taskStore, err := agent.NewTaskStore()
	if err != nil {
		panic(err)
	}
	goalStore := agent.NewGoalStoreAt(filepath.Join(filepath.Dir(taskStore.TasksDir()), "goals"))
	tasks := agent.NewTaskManager(taskStore)
	opsStore := agent.NewOpsStoreAt(filepath.Join(filepath.Dir(taskStore.TasksDir()), "incidents"))
	s := &server{version: version, assets: assets, static: http.FileServer(http.FS(assets)), checker: system.NewChecker(), plugins: setupplugin.NewRegistry(), auth: identity, ai: aiManager, agentSessions: sessionStore, agentTaskStore: taskStore, agentTasks: tasks, taskService: &agent.TaskService{Manager: tasks}, goalStore: goalStore, opsStore: opsStore, goalSchedulerStop: make(chan struct{}), goalRunning: map[string]bool{}, agentConfirms: newAgentConfirmManager(), development: map[string]developmentProcess{}, webviewRuntimes: map[string]*webViewRuntime{}, stopping: map[string]bool{}, consoleCache: map[string]consoleSnapshot{}, directoryRoots: managedDirectoryRoots(), events: newRobotEventHub()}
	s.opsOrchestrator = &agent.OpsOrchestrator{
		Store:  opsStore,
		Policy: func(root string) (agent.OpsPolicy, error) { return opsStore.GetPolicy(root) },
		AI: func(incident agent.Incident, policy agent.OpsPolicy) (agent.AutoFixDecision, error) {
			providers, err := s.ai.List()
			if err != nil {
				return agent.AutoFixDecision{}, err
			}
			prompt := `你是线上运维审查器。只返回 JSON，不要 Markdown。动作只能是 observe_only、restart_process、auto_fix、create_todo、escalate。禁止凭据、数据库、系统权限和任意 shell 操作。若定位、验证或风险不明确，返回 create_todo。字段：action,confidence,severity,risk,reason,requiresHuman,allowedActions。\n项目：` + incident.ProjectRoot + `\n错误：` + incident.Sample + `\n文件：` + incident.File + `:` + fmt.Sprint(incident.Line) + `\n策略：` + policy.Mode
			for _, provider := range providers {
				if !provider.HasKey {
					continue
				}
				answer, chatErr := s.ai.Chat(provider.ID, provider.Model, []map[string]string{{"role": "user", "content": prompt}})
				if chatErr == nil {
					return agent.ParseAutoFixDecision(answer)
				}
			}
			return agent.AutoFixDecision{}, errors.New("没有可用的 AI Provider")
		},
		PM2: func(root, action string) (string, error) {
			if !agent.IsAllowedPM2Action(action) {
				return "", fmt.Errorf("PM2 操作不在白名单：%s", action)
			}
			result, err := s.robots.Run(root, action, "", "", "", "", "", true)
			return result.Output, err
		},
		StartFix: func(incident agent.Incident, _ agent.AutoFixDecision) (string, error) {
			providers, err := s.ai.List()
			if err != nil {
				return "", err
			}
			for _, provider := range providers {
				if !provider.HasKey {
					continue
				}
				created, createErr := s.createAgentTask(agentTaskInput{Provider: provider.ID, Model: provider.Model, Root: incident.ProjectRoot, Access: "auto", Messages: []map[string]string{{"role": "user", "content": "生产错误自动维护：" + incident.Sample + "\n请先定位根因，仅修改必要文件，并运行验证命令。"}}, Isolation: "workspace"}, true)
				if createErr == nil {
					return created.ID, nil
				}
			}
			return "", errors.New("没有已配置的 AI Provider")
		},
	}
	s.opsMonitor = s.newOpsMonitor()
	_ = s.goalStore.ReconcileRuns(s.agentTasks.List())
	_ = s.opsStore.ReconcileMaintenance(s.agentTasks.List())
	s.startGoalScheduler()
	if s.opsMonitor != nil {
		_ = s.opsMonitor.Start(context.Background())
	}
	s.agentTasks.SetObserver(func(previous, current agent.AgentTask) {
		if previous.Status != current.Status && s.opsStore != nil {
			status := string(current.Status)
			if status == string(agent.TaskCompleted) || status == string(agent.TaskFailed) || status == string(agent.TaskCancelled) {
				_ = s.opsStore.UpdateMaintenanceByTask(current.ID, status, current.LastError)
				if status == string(agent.TaskCompleted) {
					if runs, runsErr := s.opsStore.ListMaintenance(); runsErr == nil {
						for _, run := range runs {
							if run.TaskID != current.ID {
								continue
							}
							if incident, incidentErr := s.opsStore.GetIncident(run.IncidentID); incidentErr == nil {
								if policy, policyErr := s.opsStore.GetPolicy(incident.ProjectRoot); policyErr == nil && policy.MaxModifiedFiles > 0 {
									if report, reportErr := s.agentTaskStore.LoadReport(current.ID); reportErr == nil && len(report.ModifiedFiles) > policy.MaxModifiedFiles {
										store := agent.NewSnapshotStoreAt(filepath.Join(s.agentTaskStore.TasksDir(), current.ID, "snapshots"))
										_, _ = store.Rollback(current.ID, incident.ProjectRoot, false)
										run.Status, run.Error, run.RollbackPerformed = "failed", fmt.Sprintf("修改文件数超过策略上限 %d", policy.MaxModifiedFiles), true
										incident.Status, incident.DecisionReason = agent.IncidentTodo, run.Error
										_ = s.opsStore.SaveIncident(incident)
									}
								}
							}
							_ = s.opsStore.SaveMaintenance(run)
						}
					}
				}
				if status == string(agent.TaskFailed) || status == string(agent.TaskCancelled) {
					if runs, runsErr := s.opsStore.ListMaintenance(); runsErr == nil {
						for _, run := range runs {
							if run.TaskID != current.ID {
								continue
							}
							if incident, incidentErr := s.opsStore.GetIncident(run.IncidentID); incidentErr == nil {
								if policy, policyErr := s.opsStore.GetPolicy(incident.ProjectRoot); policyErr == nil {
									policy.FailureCount++
									if policy.FailureCircuitBreak > 0 && policy.FailureCount >= policy.FailureCircuitBreak {
										policy.Mode, policy.AutoAllowed = "strict", false
									}
									policy.Updated = time.Now()
									_ = s.opsStore.SavePolicy(policy)
								}
							}
						}
					}
				}
			}
		}
		if current.GoalID == "" || previous.Status == current.Status {
			return
		}
		status := string(current.Status)
		if status == string(agent.TaskCompleted) || status == string(agent.TaskFailed) || status == string(agent.TaskCancelled) {
			_ = s.goalStore.UpdateRunByTask(current.ID, status, current.LastError)
		}
	})
	// Kill any previously supervised robot process that survived a restart so a
	// stray node cannot keep the app port occupied.
	cleanupStaleProcesses()
	// Poll plugin roots so adding/removing a plugin directory or editing a
	// manifest is reflected without a restart or manual refresh.
	s.pluginWatcher = s.plugins.StartWatch(time.Second)
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
	mux.HandleFunc("/api/v1/goals", s.listGoals)
	mux.HandleFunc("/api/v1/checks", s.checksHandler)
	mux.HandleFunc("/api/v1/projects", s.projectsHandler)
	mux.HandleFunc("/api/v1/releases", s.releasesHandler)
	mux.HandleFunc("/api/v1/update", s.updateHandler)
	mux.HandleFunc("/api/v1/update/download", s.downloadUpdateHandler)
	mux.HandleFunc("/api/v1/update/apply", s.applyUpdateHandler)
	mux.HandleFunc("/api/v1/update/load", s.loadUpdateHandler)
	mux.HandleFunc("/api/v1/ai/providers", s.aiProvidersHandler)
	mux.HandleFunc("/api/v1/ai/models", s.aiModelsHandler)
	mux.HandleFunc("/api/v1/ai/chat", s.aiChatHandler)
	mux.HandleFunc("/api/v1/agent/chat", s.agentChatHandler)
	mux.HandleFunc("/api/v1/agent/sessions", s.agentSessionsHandler)
	mux.HandleFunc("/api/v1/agent/sessions/", s.agentSessionHandler)
	mux.HandleFunc("/api/v1/agent/approve", s.agentConfirmHandler)
	mux.HandleFunc("/api/v1/agent/tasks", s.agentTasksHandler)
	mux.HandleFunc("/api/v1/agent/tasks/", s.agentTaskHandler)
	mux.HandleFunc("/api/v1/agent/diagnostics", s.agentDiagnosticsHandler)
	mux.HandleFunc("/api/v1/agent/goals", s.agentGoalsHandler)
	mux.HandleFunc("/api/v1/agent/goals/", s.agentGoalHandler)
	mux.HandleFunc("/api/v1/ops/", s.opsHandler)
	mux.HandleFunc("/api/v1/ops", s.opsHandler)
	mux.HandleFunc("/api/v1/system/ssh", s.sshHandler)
	mux.HandleFunc("/api/v1/system/mcp", s.systemMCPHandler)
	mux.HandleFunc("/api/v1/directories", s.directoryHandler)
	mux.HandleFunc("/api/v1/catalog", s.catalogHandler)
	mux.HandleFunc("/api/v1/catalog/versions", s.catalogVersionsHandler)
	mux.HandleFunc("/api/v1/catalog/document", s.catalogDocumentHandler)
	mux.HandleFunc("/api/v1/catalog/package-config", s.catalogPackageConfigHandler)
	mux.HandleFunc("/api/v1/setup/plugins", s.setupPluginsHandler)
	mux.HandleFunc("/api/v1/setup/plugins/revision", s.setupPluginRevisionHandler)
	mux.HandleFunc("/api/v1/setup/plugins/events", s.setupPluginEventsHandler)
	mux.HandleFunc("/api/v1/setup/plugins/", s.setupPluginActionHandler)
	mux.HandleFunc("/api/v1/setup/plugins/web/", s.setupPluginWebHandler)
	mux.HandleFunc("/api/v1/robot", s.robotHandler)
	mux.HandleFunc("/api/v1/robot/validate", s.robotValidateHandler)
	mux.HandleFunc("/api/v1/robot/console", s.robotConsoleHandler)
	mux.HandleFunc("/api/v1/robot/terminal", s.robotTerminalHandler)
	mux.HandleFunc("/api/v1/robot/pm2-logs", s.robotPM2LogsHandler)
	mux.HandleFunc("/api/v1/robot/pm2-status", s.robotPM2StatusHandler)
	mux.HandleFunc("/api/v1/robot/pm2-processes", s.robotPM2ProcessesHandler)
	mux.HandleFunc("/api/v1/robot/app-port", s.robotAppPortHandler)
	mux.HandleFunc("/api/v1/robot/app/", s.robotAppHandler)
	mux.HandleFunc("/api/v1/robot/runtime", s.robotRuntimeHandler)
	mux.HandleFunc("/api/v1/robot/runtime/preflight", s.robotRuntimePreflightHandler)
	mux.HandleFunc("/api/v1/robot/tasks", s.robotTasksHandler)
	mux.HandleFunc("/api/v1/robot/events", s.robotEventsHandler)
	mux.HandleFunc("/api/v1/robot/packages", s.robotPackagesHandler)
	mux.HandleFunc("/api/v1/robot/package-versions", s.robotPackageVersionsHandler)
	mux.HandleFunc("/api/v1/robot/package-readme", s.robotPackageReadmeHandler)
	mux.HandleFunc("/api/v1/robot/webviews", s.robotWebViewsHandler)
	mux.HandleFunc("/api/v1/robot/webview/", s.robotWebViewHandler)
	mux.HandleFunc("/api/v1/robot/apps", s.robotAppsHandler)
	mux.HandleFunc("/api/v1/robot/package-config", s.robotPackageConfigHandler)
	mux.HandleFunc("/api/v1/robot/login", s.robotLoginHandler)
	mux.HandleFunc("/api/v1/robot/manifest", s.robotManifestHandler)
	mux.HandleFunc("/api/v1/robot/git-init", s.robotGitInitHandler)
	mux.HandleFunc("/api/v1/robot/git", s.robotGitHandler)
	mux.HandleFunc("/api/v1/robot/git-clone", s.robotGitCloneHandler)
	mux.HandleFunc("/api/v1/robot/git-clone/check", s.robotGitCloneCheckHandler)
	mux.HandleFunc("/api/v1/robot/git-clone/branches", s.robotGitCloneBranchesHandler)
	mux.HandleFunc("/api/v1/publish/npm/status", s.npmPublishStatusHandler)
	mux.HandleFunc("/api/v1/publish/npm/pack", s.npmPackPreviewHandler)
	mux.HandleFunc("/api/v1/publish/git/status", s.gitPublishStatusHandler)
	mux.HandleFunc("/api/v1/publish/git/refresh-branches", s.gitPublishRefreshBranchesHandler)
	mux.HandleFunc("/api/v1/publish/git/prepare", s.gitBuildPrepareHandler)
	mux.HandleFunc("/api/v1/publish/git/publish", s.gitBuildPublishHandler)
	mux.HandleFunc("/api/v1/publish/git/retry-tag", s.gitBuildRetryTagHandler)
	mux.Handle("/", s.spa())
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery(), s.ginHeaders(), s.ginAccess(), s.ginRequestLog())
	// The Gin engine owns every request. Existing handlers remain standard
	// net/http functions, preserving their API contracts during migration.
	router.Any("/*path", gin.WrapH(mux))
	return &ServerRuntime{Handler: router, server: s}
}

const authCookieName = "alx_session"

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

func (s *server) gitPublishRefreshBranchesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Root string `json:"root"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别。")
		return
	}
	status, err := robot.RefreshGitSourceBranches(input.Root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *server) gitBuildPrepareHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Root   string `json:"root"`
		Branch string `json:"branch"`
		Commit string `json:"commit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别。")
		return
	}
	session, err := robot.PrepareGitBuild(input.Root, input.Branch, input.Commit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *server) gitBuildPublishHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		SessionID string   `json:"sessionId"`
		Version   string   `json:"version"`
		Artifacts []string `json:"artifacts"`
		Confirm   bool     `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别。")
		return
	}
	result, err := robot.PublishPreparedGitBuild(input.SessionID, input.Version, input.Artifacts, input.Confirm)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) gitBuildRetryTagHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别。")
		return
	}
	result, err := robot.RetryPreparedGitTag(input.SessionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
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

// setupPluginRevisionHandler exposes the plugin registry revision so the UI can
// cheaply detect hot-plugged plugin changes without re-fetching the whole list.
func (s *server) setupPluginRevisionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": s.plugins.Revision()})
}

// setupPluginEventsHandler streams a "plugins changed" event over SSE whenever
// the plugin registry revision bumps, so the UI can refresh the plugin list
// without polling it. The event carries no payload; the client refetches
// /setup/plugins on receipt.
func (s *server) setupPluginEventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "SSE 不受支持。")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	sub := s.plugins.Subscribe()
	defer s.plugins.Unsubscribe(sub)
	// Heartbeat keeps proxies from closing the idle connection.
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-sub:
			if _, err := w.Write([]byte("data: {}\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
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
	if parts[1] == "install" {
		installed, err := s.plugins.Install(parts[0])
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": installed.ID, "installed": true})
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

// setupPluginWebHandler serves a setup plugin's static web UI. It is served
// same-origin so the UI can call the plugin's own action API directly. Only
// installed, enabled plugins are served: an online-only plugin is read-only
// and must be installed locally before its web assets exist. Path traversal
// and symlink escapes are rejected before any file is read.
func (s *server) setupPluginWebHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	const prefix = "/api/v1/setup/plugins/web/"
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, prefix), "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "未找到插件 Web 界面。")
		return
	}
	plugin, err := s.plugins.Find(parts[0])
	if err != nil || plugin.Online || !plugin.Runnable || !plugin.Enabled {
		writeError(w, http.StatusNotFound, "未找到插件 Web 界面。")
		return
	}
	root, err := plugin.WebRoot()
	if err != nil {
		writeError(w, http.StatusNotFound, "插件 Web 界面不可用。")
		return
	}
	requestPath := ""
	if len(parts) == 2 {
		requestPath = parts[1]
	}
	requestPath = strings.TrimPrefix(filepath.ToSlash(requestPath), "/")
	if requestPath == "" {
		requestPath = "index.html"
	}
	clean := filepath.Clean(requestPath)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		writeError(w, http.StatusNotFound, "插件 Web 资源路径无效。")
		return
	}
	candidate := filepath.Join(root, clean)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		// A no-extension path is treated as an SPA route served by index.html.
		if filepath.Ext(clean) == "" {
			resolved = filepath.Join(root, "index.html")
		} else {
			writeError(w, http.StatusNotFound, "插件 Web 资源不存在。")
			return
		}
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		writeError(w, http.StatusNotFound, "插件 Web 资源路径无效。")
		return
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, "插件 Web 资源不存在。")
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Same-origin UI that may call the plugin action API. The shipped plugin
	// pages (e.g. alemonx-qq) use inline scripts, so script-src must allow
	// 'unsafe-inline'. frame-ancestors keeps it embeddable only from the
	// loopback management UI.
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data: blob: https: http:; style-src 'self' 'unsafe-inline'; frame-ancestors http://localhost:* http://127.0.0.1:*; base-uri 'none'")
	http.ServeFile(w, r, resolved)
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

// systemMCPHandler reports whether the local HTTP MCP server is actually
// running, so the header control shows a real status instead of pretending the
// service is always up. MCP is started manually via `alx mcp-http`.
func (s *server) systemMCPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"running": probeMCPRunning()})
}

// probeMCPRunning probes the default Streamable HTTP MCP endpoint. Any HTTP
// response (even 401/404) means a server is listening; only a connection error
// means the service is not running.
func probeMCPRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:17391/mcp", nil)
	if err != nil {
		return false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return true
}

func (s *server) directoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	roots := s.currentDirectoryRoots()
	if r.Method != http.MethodGet {
		var input struct {
			Path string `json:"path"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "目录操作内容无法识别。")
			return
		}
		path, err := managedDirectory(input.Path, roots)
		if err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		if r.Method == http.MethodPost {
			name := strings.TrimSpace(input.Name)
			if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
				writeError(w, http.StatusBadRequest, "文件夹名称无效。")
				return
			}
			created := filepath.Join(path, name)
			if err := os.Mkdir(created, 0755); err != nil {
				if os.IsExist(err) {
					writeError(w, http.StatusConflict, "同名文件夹已存在。")
				} else {
					writeError(w, http.StatusBadRequest, "无法新建文件夹："+err.Error())
				}
				return
			}
			writeJSON(w, http.StatusCreated, map[string]string{"path": created})
			return
		}
		if input.Path == "" || filepath.Clean(path) == filepath.Clean(input.Path) && func() bool {
			for _, root := range roots {
				if filepath.Clean(path) == filepath.Clean(root) {
					return true
				}
			}
			return false
		}() {
			writeError(w, http.StatusBadRequest, "不能删除管理范围的根目录。")
			return
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			writeError(w, http.StatusBadRequest, "目标文件夹不存在。")
			return
		}
		if err := os.RemoveAll(path); err != nil {
			writeError(w, http.StatusBadRequest, "无法删除文件夹："+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"path": path})
		return
	}
	path, err := managedDirectory(r.URL.Query().Get("path"), roots)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsPermission(err) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "没有读取此位置的权限。请在系统设置中为 alx 授予“文件与文件夹”或“完全磁盘访问”权限，然后重试。", "permission": true})
			return
		}
		writeError(w, http.StatusBadRequest, "无法读取该目录")
		return
	}
	type directory struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	type file struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	showHidden := r.URL.Query().Get("hidden") == "true"
	includeFiles := r.URL.Query().Get("files") == "true"
	directories := make([]directory, 0)
	files := make([]file, 0)
	for _, entry := range entries {
		if entry.IsDir() && (showHidden || !strings.HasPrefix(entry.Name(), ".")) {
			directories = append(directories, directory{Name: entry.Name(), Path: filepath.Join(path, entry.Name())})
		}
		if includeFiles && !entry.IsDir() && (showHidden || !strings.HasPrefix(entry.Name(), ".")) {
			files = append(files, file{Name: entry.Name(), Path: filepath.Join(path, entry.Name())})
		}
	}
	sort.Slice(directories, func(i, j int) bool { return directories[i].Name < directories[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	parent := ""
	for _, root := range roots {
		if filepath.Clean(path) != filepath.Clean(root) {
			if next := filepath.Dir(path); isWithinRoot(next, root) {
				parent = next
			}
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "parent": parent, "roots": roots, "locations": directoryLocations(roots), "directories": directories, "files": files})
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
		name, kind := directoryLocation(root, home, runtime.GOOS)
		items = append(items, map[string]string{"name": name, "path": root, "kind": kind})
	}
	return items
}

// directoryLocation gives filesystem roots a human-readable name. In
// particular, filepath.Base(`C:\\`) on Windows can resolve to a separator,
// which used to leave users with a meaningless “/” disk entry.
func directoryLocation(root, home, goos string) (string, string) {
	clean := filepath.Clean(root)
	if clean == filepath.Clean(home) {
		return "主目录", "home"
	}
	if goos == "windows" {
		volume := filepath.VolumeName(root)
		// Keep this testable on non-Windows hosts and robust for paths received
		// from a Windows client in their native form.
		if volume == "" && len(root) >= 2 && root[1] == ':' {
			volume = root[:2]
		}
		if volume != "" {
			return "本地磁盘（" + strings.ToUpper(volume) + "）", "volume"
		}
		return "本地磁盘", "volume"
	}
	if clean == string(filepath.Separator) {
		return "系统磁盘", "disk"
	}
	if goos == "darwin" && strings.HasPrefix(clean, "/Volumes/") {
		return filepath.Base(clean), "volume"
	}
	if goos == "linux" && (strings.HasPrefix(clean, "/media/") || strings.HasPrefix(clean, "/run/media/")) {
		return filepath.Base(clean), "volume"
	}
	return filepath.Base(clean), "disk"
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
	if update.Available && update.PlatformMatched && update.IntegrityReady {
		_, update.DownloadReady, err = system.CachedUpdate(update.AssetName, update.SHA256)
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
		Model    string              `json:"model"`
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
	answer, err := s.ai.Chat(input.Provider, input.Model, input.Messages)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"answer": answer})
}

func (s *server) aiModelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	items, err := s.ai.Models(r.URL.Query().Get("provider"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": items})
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
	if !update.PlatformMatched || !update.IntegrityReady {
		if update.PlatformMatched {
			writeError(w, http.StatusBadRequest, "该版本未提供校验文件，无法安全地自动更新，请使用手动安装。")
			return
		}
		writeError(w, http.StatusBadRequest, "未找到当前系统对应的更新包，请使用手动更新。")
		return
	}
	if _, err := system.DownloadUpdate(update.DownloadURL, update.AssetName, update.SHA256, update.Latest); err != nil {
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
	update, path, ready, err := system.ReadyPendingUpdate()
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
	if err := system.ClearPendingUpdate(); err != nil {
		log.Printf("更新已完成，但无法清理更新元数据：%v", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": result + " 正在重启应用。", "version": update.Version})
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
	directory, err := os.MkdirTemp("", "alx-upload-")
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

// consolePayload splits the terminal's live process output from its static
// project context. The two have different update rates, so returning them as
// one blob forces the browser to re-render the expensive snapshot on every poll.
type consolePayload struct {
	Path     string `json:"path"`
	Output   string `json:"output"`
	Snapshot string `json:"snapshot"`
	Mode     string `json:"mode"`
	Running  bool   `json:"running"`
}

func (s *server) robotConsoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	root := r.URL.Query().Get("root")
	// The refresh flag bypasses the snapshot cache so a manual terminal refresh
	// reflects just-saved package.json / config changes instead of stale context.
	var console robot.Result
	var err error
	if r.URL.Query().Get("refresh") == "1" {
		console, err = s.robots.Console(root)
		if err == nil {
			s.mu.Lock()
			s.consoleCache[root] = consoleSnapshot{output: console.Output, at: time.Now()}
			s.mu.Unlock()
		}
	} else {
		console, err = s.cachedRobotConsole(root)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Prefer the process that is actually running; otherwise show the most
	// recent dev/app run. A fixed "app first, then dev" order would hide a fresh
	// dev run whenever an older foreground run still has history.
	output, status, runError, mode := s.runtimeProcessOutput(root)
	payload := consolePayload{Path: root, Snapshot: console.Output, Mode: mode}
	switch {
	case status == "running":
		payload.Output = "$ " + mode + "实时输出\n" + output
		payload.Running = true
	case output != "":
		payload.Output = "$ 最近一次" + mode + "输出\n" + output
		if status == "failed" && runError != "" {
			// runError already reads like "开发进程已退出：exit status 1".
			// Appending it as its own paragraph avoids a redundant heading.
			payload.Output += "\n\n" + runError
		}
	default:
		payload.Output = "当前没有正在运行的前台或开发进程。"
	}
	writeJSON(w, http.StatusOK, payload)
}

type robotTerminalRequest struct {
	Root    string `json:"root"`
	Command string `json:"command"`
}

// robotTerminalHandler runs one approved command with the robot directory as
// its working directory. It deliberately does not expose a persistent shell:
// every request is validated and scoped independently before it starts.
func (s *server) robotTerminalHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input robotTerminalRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "终端命令格式无效。")
		return
	}
	validation, err := (robot.Manager{}).Validate(input.Root)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	root := validation.Path
	command := strings.TrimSpace(input.Command)
	if err := validateRobotTerminalCommand(command); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-lc", command)
	}
	cmd.Dir = root
	output, runErr := cmd.CombinedOutput()
	text := string(output)
	if runErr != nil {
		if ctx.Err() != nil {
			text += "\n命令已超时并被终止。"
		} else {
			text += "\n" + runErr.Error()
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": "$ " + command + "\n" + text})
}

func validateRobotTerminalCommand(command string) error {
	if command == "" {
		return fmt.Errorf("请输入终端命令。")
	}
	if len(command) > 2000 {
		return fmt.Errorf("终端命令不能超过 2000 个字符。")
	}
	for _, token := range []string{";", "&&", "||", "|", ">", "<", "`", "$(", "${", "\n", "\r"} {
		if strings.Contains(command, token) {
			return fmt.Errorf("终端不支持 shell 拼接或重定向；请一次执行一条命令。")
		}
	}
	fields := strings.Fields(command)
	allowed := map[string]bool{
		"pwd": true, "ls": true, "dir": true, "cat": true, "head": true,
		"tail": true, "find": true, "grep": true, "git": true, "node": true,
		"npm": true, "yarn": true, "pnpm": true, "bun": true, "go": true,
		"python": true, "python3": true,
	}
	if len(fields) == 0 || !allowed[strings.ToLower(filepath.Base(fields[0]))] {
		return fmt.Errorf("该命令不在机器人目录终端的允许列表中。")
	}
	for _, field := range fields {
		if filepath.IsAbs(field) || strings.Contains(field, "..") {
			return fmt.Errorf("终端命令只能使用机器人目录内的相对路径。")
		}
		lower := strings.ToLower(field)
		if lower == "--global" || lower == "-g" || lower == "--prefix" || lower == "-c" || lower == "-e" {
			return fmt.Errorf("终端不允许全局修改或内联脚本参数。")
		}
	}
	return nil
}

// cachedRobotConsole reuses the terminal's static context for a short window so
// the polling loop never spawns git/node just to render an unchanged header.
func (s *server) cachedRobotConsole(root string) (robot.Result, error) {
	s.mu.RLock()
	cached, ok := s.consoleCache[root]
	s.mu.RUnlock()
	if ok && time.Since(cached.at) < 15*time.Second {
		return robot.Result{Path: root, Output: cached.output}, nil
	}
	result, err := s.robots.Console(root)
	if err != nil {
		return robot.Result{}, err
	}
	s.mu.Lock()
	s.consoleCache[root] = consoleSnapshot{output: result.Output, at: time.Now()}
	s.mu.Unlock()
	return result, nil
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

func (s *server) robotPM2StatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	result, err := s.robots.PM2Status(r.URL.Query().Get("root"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) robotPM2ProcessesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	result, err := s.robots.PM2Processes(r.URL.Query().Get("root"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (s *server) robotAppPortHandler(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root == "" {
		writeError(w, http.StatusBadRequest, "请选择机器人目录。")
		return
	}
	if r.Method == http.MethodGet {
		if r.URL.Query().Get("probe") == "1" {
			reachable, port, err := s.robots.AppPortReachable(root)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"reachable": reachable, "port": port})
			return
		}
		info, err := s.robots.AppPort(root)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, info)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Port int `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别。")
		return
	}
	result, err := s.robots.SaveAppPort(root, input.Port)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) robotAppsHandler(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root == "" {
		writeError(w, http.StatusBadRequest, "请选择机器人目录。")
		return
	}
	if r.Method == http.MethodGet {
		apps, err := s.robots.EnabledApps(root)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": apps})
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Package string `json:"package"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容无法识别。")
		return
	}
	result, err := s.robots.SetAppEnabled(root, input.Package, input.Enabled)
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

// robotEventsHandler streams task-state and process-output events over SSE so
// the UI can update live without polling robot/tasks and robot/console.
func (s *server) robotEventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "SSE 不受支持。")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	sub := s.events.subscribe()
	defer s.events.unsubscribe(sub)
	// Heartbeat keeps proxies from closing the idle connection.
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case event := <-sub:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
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
		Value   string `json:"value"`
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
		if !s.developmentRunning(input.Root) {
			finished := time.Now()
			created.Status = "completed"
			created.Output = "当前没有正在运行的" + mode + "进程。"
			created.FinishedAt = &finished
			s.addOperation(created)
			writeJSON(w, http.StatusAccepted, created)
			return
		}
		// The stop task stays "running" until the process actually exits, so the
		// UI shows a real "正在停止" state instead of claiming success early.
		created.Status = "running"
		created.Output = "正在请求停止" + mode + "…"
		s.addOperation(created)
		if !s.stopDevelopment(input.Root, mode) {
			// The process vanished between the check and the stop request. Finish
			// the task immediately so it never hangs as "running" forever.
			finished := time.Now()
			s.mu.Lock()
			for index := range s.operations {
				if s.operations[index].ID == created.ID {
					s.operations[index].Status = "completed"
					s.operations[index].Output = "进程已退出，无需停止。"
					s.operations[index].FinishedAt = &finished
					break
				}
			}
			s.mu.Unlock()
		}
		writeJSON(w, http.StatusAccepted, created)
		return
	}
	if input.Action == "dev" || input.Action == "app" {
		// Starting a robot owns the dependency lifecycle. A new clone or a
		// changed workspace must not make beginners detour to a separate
		// "install dependencies" button before they can run it.
		dependencyOutput, dependencyErr := s.robots.EnsureRuntimeDependencies(input.Root)
		if dependencyErr != nil {
			writeError(w, http.StatusBadRequest, dependencyErr.Error())
			return
		}
		if s.developmentRunning(input.Root) {
			writeError(w, http.StatusConflict, "当前目录已有前台或开发进程正在运行；请先停止后再启动。")
			return
		}
		// "谁最后启动谁为准": a running background PM2 service would keep the
		// app port, so a local start first stops PM2, waits for the port to be
		// released, then starts the local process.
		if err := s.stopPM2ForStart(input.Root); err != nil {
			writeError(w, http.StatusConflict, "停止后台（PM2）服务失败："+err.Error())
			return
		}
		if !s.waitPortFree(input.Root, 0) {
			// The old process could not release the port gracefully. Kill the
			// listener outright so the new start always wins the port.
			if err := s.forceFreePort(input.Root); err != nil {
				writeError(w, http.StatusConflict, "应用端口仍被占用："+err.Error())
				return
			}
		}
		command, err := s.robots.DevelopmentCommand(input.Root)
		if input.Action == "app" {
			command, err = s.robots.ForegroundCommand(input.Root)
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		configureManagedProcess(command)
		// Route stdout/stderr through a writer instead of StdoutPipe. exec copies
		// process output into the writer on its own goroutine, so when the
		// process exits the copy ends with a clean EOF rather than the pipe
		// being closed underneath a concurrent reader (the "read |0: file
		// already closed" error). It also cannot lose the final lines.
		command.Stdout = newOperationWriter(created.ID, s)
		command.Stderr = newOperationWriter(created.ID, s)
		if err := command.Start(); err != nil {
			writeError(w, http.StatusBadRequest, "运行启动失败："+err.Error())
			return
		}
		if !s.registerDevelopment(input.Root, created.ID, input.Action, command, processGroupID(command)) {
			_ = command.Process.Kill()
			writeError(w, http.StatusConflict, "当前目录已有前台或开发进程正在运行；请先停止后再启动。")
			return
		}
		created.Output = strings.TrimSpace(dependencyOutput)
		if created.Output != "" {
			created.Output += "\n"
		}
		created.Output += map[bool]string{true: "开发模式已启动，正在等待进程输出…\n", false: "前台运行已启动，正在等待进程输出…\n"}[input.Action == "dev"]
		s.addOperation(created)
		log.Printf("[ROBOT %s] 开始 action=%s root=%q", created.ID, input.Action, created.Root)
		go s.watchDevelopmentTask(created.ID, input.Root, input.Action, command)
		writeJSON(w, http.StatusAccepted, created)
		return
	}
	if input.Action == "pm2" || input.Action == "pm2-reload" {
		// "谁最后启动谁为准": a running local process holds the app port, so a
		// background start first stops the local process and waits for release.
		s.stopLocalForStart(input.Root)
		if !s.waitPortFree(input.Root, 0) {
			if err := s.forceFreePort(input.Root); err != nil {
				writeError(w, http.StatusConflict, "应用端口仍被占用："+err.Error())
				return
			}
		}
	}
	log.Printf("[ROBOT %s] 开始 action=%s root=%q", created.ID, created.Action, created.Root)
	s.addOperation(created)
	go func() {
		result, err := s.robots.Run(input.Root, input.Action, input.Message, input.Package, input.Version, input.Tag, input.Token, input.Confirm == "true")
		finished := time.Now()
		s.mu.Lock()
		var snapshot operationTask
		for index := range s.operations {
			if s.operations[index].ID == created.ID {
				s.operations[index].Status = "completed"
				s.operations[index].Output = result.Output
				s.operations[index].FinishedAt = &finished
				if err != nil {
					s.operations[index].Status = "failed"
					s.operations[index].Error = err.Error()
				}
				snapshot = s.operations[index]
				break
			}
		}
		s.mu.Unlock()
		if snapshot.ID != "" {
			s.events.publish(robotEvent{Type: "task", TaskID: snapshot.ID, Task: &snapshot})
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

func (s *server) operationOutput(root, action string) (output, status, runError string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.operations {
		if item.Root == root && item.Action == action {
			return item.Output, item.Status, item.Error
		}
	}
	return "", "", ""
}

// runtimeProcessOutput returns the dev/app process output for a root that the
// terminal should show: the currently running process if there is one,
// otherwise the most recent run (operations are kept newest-first).
func (s *server) runtimeProcessOutput(root string) (output, status, runError, mode string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latestOutput, latestStatus, latestError, latestMode string
	for _, item := range s.operations {
		if item.Root != root || (item.Action != "dev" && item.Action != "app") {
			continue
		}
		if latestOutput == "" {
			latestOutput, latestStatus, latestError = item.Output, item.Status, item.Error
			latestMode = map[string]string{"dev": "开发模式", "app": "前台运行"}[item.Action]
		}
		if item.Status == "running" {
			return item.Output, item.Status, item.Error, map[string]string{"dev": "开发模式", "app": "前台运行"}[item.Action]
		}
	}
	return latestOutput, latestStatus, latestError, latestMode
}

// pm2StatusFor returns the PM2 state, or an error after a short window. A
// bounded read keeps a broken pm2 install (which would otherwise trigger a
// package-manager download) from holding up a local start request for minutes.
func (s *server) pm2StatusFor(root string) (robot.PM2Status, error) {
	if s.pm2Status != nil {
		return s.pm2Status(root)
	}
	return s.pm2StatusWithin(root, 3*time.Second)
}

func (s *server) pm2StatusWithin(root string, window time.Duration) (robot.PM2Status, error) {
	done := make(chan pm2StatusResult, 1)
	go func() {
		status, err := s.robots.PM2Status(root)
		done <- pm2StatusResult{status: status, err: err}
	}()
	select {
	case result := <-done:
		return result.status, result.err
	case <-time.After(window):
		// The goroutine may still be reading; it only queries PM2 and never
		// mutates server state, so it is safe to abandon.
		return robot.PM2Status{}, fmt.Errorf("读取 PM2 状态超时")
	}
}

// localStartBlockedByPM2 reports whether a local (dev/app) process must not be
// started for a root. This is strict: when PM2 is configured but its state
// cannot be read, we block rather than risk a port clash with a running PM2
// service. Only a PM2Status that definitively reports not running allows a
// local start.
func (s *server) localStartBlockedByPM2(root string) (string, bool) {
	status, err := s.pm2StatusFor(root)
	if err != nil {
		return "无法读取后台（PM2）服务状态；为免端口冲突，请先在“后台运行”中确认服务已停止，再启动本机进程。", true
	}
	if status.Running {
		return "当前目录正在后台（PM2）运行；请先在“后台运行”中停止服务，再启动本机进程。", true
	}
	return "", false
}

// pm2StartBlockedByLocal reports whether a local process is running and
// therefore blocks starting the background PM2 service for the same root.
func (s *server) pm2StartBlockedByLocal(root string) (string, bool) {
	if !s.developmentRunning(root) {
		return "", false
	}
	return "当前目录正在本机（开发/前台）运行；请先停止本机进程，再启动后台服务。", true
}

// stopPM2ForStart stops the background PM2 service for a root so a new local
// process can take over the app port ("last one to start wins"). It tolerates
// a PM2 state read failure: if the config exists we attempt the stop anyway.
func (s *server) stopPM2ForStart(root string) error {
	status, err := s.pm2StatusFor(root)
	if err == nil && !status.Running {
		return nil
	}
	// pm2-delete removes the process entirely, which is more reliable than a
	// graceful stop for guaranteeing the port is released before a local start.
	_, stopErr := s.robots.Run(root, "pm2-delete", "", "", "", "", "", false)
	return stopErr
}

// stopLocalForStart stops a supervised local (dev/app) process so a new
// background PM2 service can take over the app port.
func (s *server) stopLocalForStart(root string) error {
	if !s.developmentRunning(root) {
		return nil
	}
	s.stopDevelopment(root, "本机进程")
	return nil
}

// waitPortFree polls the configured application port until the previous
// process has released it. It retries for ~12 seconds so a graceful stop that
// needs its 3-second force-stop fallback has time to finish.
func (s *server) waitPortFree(root string, attempts int) bool {
	if attempts <= 0 {
		attempts = 30
	}
	info, err := s.robots.AppPort(root)
	if err != nil {
		return true
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for i := 0; i < attempts; i++ {
		response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(info.Port))
		if err != nil {
			return true // connection refused => port free
		}
		_ = response.Body.Close()
		time.Sleep(400 * time.Millisecond)
	}
	return false
}

// forceFreePort kills whatever is listening on the configured application port.
// This is the final "谁最后启动谁为准" fallback: the old process (PM2, a
// supervised dev/app, or a stray node) could not release the port gracefully,
// so we identify the listener and terminate it. Only the robot's configured
// serverPort is targeted, never an arbitrary port.
func (s *server) forceFreePort(root string) error {
	info, err := s.robots.AppPort(root)
	if err != nil {
		return err
	}
	output, err := s.runCommandForPort("lsof", "-ti", "tcp:"+strconv.Itoa(info.Port))
	if err != nil || strings.TrimSpace(output) == "" {
		return fmt.Errorf("未找到占用端口 %d 的进程", info.Port)
	}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		pid := strings.TrimSpace(line)
		if pid == "" || !isNumeric(pid) {
			continue
		}
		_, _ = s.runCommandForPort("kill", "-9", pid)
	}
	// Re-check after killing.
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for i := 0; i < 10; i++ {
		response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(info.Port))
		if err != nil {
			return nil
		}
		_ = response.Body.Close()
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("端口 %d 仍被占用", info.Port)
}

// runCommandForPort runs a small helper command without a managed process
// group; used only by forceFreePort.
func (s *server) runCommandForPort(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.Output()
	return string(output), err
}

func isNumeric(value string) bool {
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return value != ""
}

func (s *server) developmentRunning(root string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, running := s.development[root]
	return running
}

func (s *server) registerDevelopment(root, taskID, action string, command *exec.Cmd, pgid int) bool {
	s.mu.Lock()
	if _, running := s.development[root]; running {
		s.mu.Unlock()
		return false
	}
	s.development[root] = developmentProcess{TaskID: taskID, Command: command, PGID: pgid}
	s.mu.Unlock()
	s.recordProcess(root, taskID, pgid, action)
	return true
}

// recordProcess appends a persisted marker for a supervised robot process so a
// stray node can be located and killed even after an alx restart.
func (s *server) recordProcess(root, taskID string, pgid int, action string) {
	if pgid <= 0 {
		return
	}
	s.mu.Lock()
	items := loadPersistedProcesses()
	items = append(items, persistedProcess{Root: root, TaskID: taskID, PGID: pgid, Action: action, StartedAt: time.Now()})
	savePersistedProcesses(items)
	s.mu.Unlock()
}

// forgetProcess removes the persisted marker for a process that has exited.
func (s *server) forgetProcess(root, taskID string) {
	s.mu.Lock()
	items := loadPersistedProcesses()
	kept := items[:0]
	for _, item := range items {
		if item.Root == root && item.TaskID == taskID {
			continue
		}
		kept = append(kept, item)
	}
	savePersistedProcesses(kept)
	s.mu.Unlock()
}

// stopDevelopment requests a graceful stop of the supervised process and
// reports whether a process was actually running. A false result lets the
// caller finish its stop task immediately instead of waiting for an exit that
// already happened.
func (s *server) stopDevelopment(root, mode string) bool {
	s.mu.Lock()
	process, running := s.development[root]
	if !running {
		s.mu.Unlock()
		return false
	}
	s.stopping[root] = true
	s.mu.Unlock()
	s.appendOperationOutput(process.TaskID, "正在请求停止"+mode+"…\n")
	if err := interruptManagedProcess(process.Command); err != nil {
		// Do not abandon a stop merely because the graceful signal raced with a
		// child exit or the package manager rejected it. The force-stop pass
		// below still targets the complete process group.
		s.appendOperationOutput(process.TaskID, "优雅停止未确认，继续执行强制停止兜底。\n")
	}
	// Yarn and similar package managers can leave a child process alive after
	// their own interrupt. Give the group a brief graceful window, then stop
	// every member rather than only its package-manager parent.
	time.AfterFunc(3*time.Second, func() {
		s.mu.RLock()
		current, active := s.development[root]
		stopping := s.stopping[root]
		s.mu.RUnlock()
		if active && stopping && current.TaskID == process.TaskID {
			s.appendOperationOutput(process.TaskID, "进程仍未退出，正在强制停止全部子进程…\n")
			_ = forceStopManagedProcess(current.Command)
		}
	})
	return true
}

func (s *server) appendOperationOutput(id, output string) {
	if output == "" {
		return
	}
	const maxOutput = 256 * 1024
	s.mu.Lock()
	updated := false
	for index := range s.operations {
		if s.operations[index].ID != id {
			continue
		}
		s.operations[index].Output += output
		if len(s.operations[index].Output) > maxOutput {
			s.operations[index].Output = "…前面的输出已省略…\n" + s.operations[index].Output[len(s.operations[index].Output)-maxOutput:]
		}
		updated = true
		break
	}
	s.mu.Unlock()
	if updated {
		// Fan out the incremental text outside the lock so SSE clients stream
		// the terminal output without a polling loop.
		s.events.publish(robotEvent{Type: "output", TaskID: id, Text: output})
	}
}

// operationWriter forwards a supervised process's output into its operation
// record. It buffers partial lines so a chunked write that splits a "\n" is
// not rendered as two fragments, and it is safe for concurrent writes from the
// separate stdout/stderr copy goroutines.
type operationWriter struct {
	id      string
	server  *server
	buffer  []byte
	writeMu sync.Mutex
}

func newOperationWriter(id string, s *server) *operationWriter {
	return &operationWriter{id: id, server: s}
}

func (w *operationWriter) Write(data []byte) (int, error) {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	written := len(data)
	w.buffer = append(w.buffer, data...)
	for {
		index := bytes.IndexByte(w.buffer, '\n')
		if index < 0 {
			break
		}
		w.server.appendOperationOutput(w.id, string(w.buffer[:index+1]))
		w.buffer = w.buffer[index+1:]
	}
	return written, nil
}

func (s *server) watchDevelopmentTask(id, root, action string, command *exec.Cmd) {
	err := command.Wait()

	finished := time.Now()
	s.mu.Lock()
	stopped := s.stopping[root]
	wasManaged := false
	if current, active := s.development[root]; active && current.TaskID == id {
		delete(s.development, root)
		delete(s.stopping, root)
		wasManaged = true
	}
	// A pending stop task only becomes "completed" once the process has really
	// exited, which is exactly the moment we reach here.
	s.completePendingStopTasks(root, finished)
	var snapshot operationTask
	for index := range s.operations {
		if s.operations[index].ID != id {
			continue
		}
		s.operations[index].FinishedAt = &finished
		processName := map[string]string{"app": "前台进程", "dev": "开发进程"}[action]
		if processName == "" {
			processName = "托管进程"
		}
		if err != nil && !stopped {
			s.operations[index].Status = "failed"
			s.operations[index].Error = processName + "已退出：" + err.Error()
			log.Printf("[ROBOT %s] %s退出 error=%s", id, processName, err)
		} else if stopped {
			s.operations[index].Status = "completed"
			s.operations[index].Output += processName + "已停止。\n"
			log.Printf("[ROBOT %s] %s已停止", id, processName)
		} else {
			s.operations[index].Status = "completed"
			s.operations[index].Output += processName + "已正常退出。\n"
			log.Printf("[ROBOT %s] %s正常退出", id, processName)
		}
		snapshot = s.operations[index]
		break
	}
	s.mu.Unlock()
	if wasManaged {
		s.forgetProcess(root, id)
	}
	if snapshot.ID != "" {
		s.events.publish(robotEvent{Type: "task", TaskID: snapshot.ID, Task: &snapshot})
	}
}

// completePendingStopTasks marks every in-flight dev-stop/app-stop task for a
// root as completed once the supervised process has really exited.
func (s *server) completePendingStopTasks(root string, finished time.Time) {
	for index := range s.operations {
		item := &s.operations[index]
		if item.Root != root || item.Status != "running" {
			continue
		}
		if item.Action != "dev-stop" && item.Action != "app-stop" {
			continue
		}
		item.Status = "completed"
		item.FinishedAt = &finished
		item.Output = "已停止" + map[string]string{"dev-stop": "开发模式", "app-stop": "前台运行"}[item.Action] + "。"
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
	if requestPath == "message" {
		s.robotWebViewMessageHandler(w, r, string(rootBytes), parts[1])
		return
	}
	if requestPath == "events" {
		s.robotWebViewEventsHandler(w, r, string(rootBytes), parts[1])
		return
	}
	if requestPath == "bridge.js" {
		entry, entryErr := s.robots.WebViewEntry(string(rootBytes), parts[1])
		if entryErr != nil {
			writeError(w, http.StatusNotFound, entryErr.Error())
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = io.WriteString(w, webViewBridge(entry))
		return
	}
	file, err := s.robots.WebViewFile(string(rootBytes), parts[1], requestPath)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	// Each WebView is opened through a robot-specific *.localhost hostname.
	// It remains loopback-only while keeping plugin storage and cookies separate
	// from both the management UI and other robots. Plugin actions can only use
	// the narrowly scoped WebView API proxy below.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// A robot plugin UI may talk only to its registered local bot API proxy.
	// Do not permit the parent management origin in connect-src.
	w.Header().Set("Content-Security-Policy", "default-src 'self' data: blob:; connect-src 'self' ws: wss:; img-src 'self' data: blob: https: http:; style-src 'self' 'unsafe-inline'; frame-ancestors http://localhost:* http://127.0.0.1:*; base-uri 'none'")
	// Vite's default production output commonly uses root-absolute local asset
	// URLs. Inside a plugin mount those would otherwise point at setup's own
	// assets, so make the common HTML, favicon and CSS bundle forms relative.
	// Ordinary external URLs remain untouched.
	if filepath.Ext(file) == ".html" {
		data, readErr := os.ReadFile(file)
		if readErr != nil {
			writeError(w, http.StatusNotFound, "插件 Web 页面不存在。")
			return
		}
		content := rewriteWebViewHTML(string(data))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, content)
		return
	}
	http.ServeFile(w, r, file)
}

// rewriteWebViewHTML keeps a plugin's common Vite assets inside its mounted
// WebView route and injects only the restricted setup bridge.
func rewriteWebViewHTML(content string) string {
	content = strings.NewReplacer(
		`src="/assets/`, `src="assets/`,
		`href="/assets/`, `href="assets/`,
		`src='/assets/`, `src='assets/`,
		`href='/assets/`, `href='assets/`,
		`href="/favicon.ico"`, `href="favicon.ico"`,
		`href='/favicon.ico'`, `href='favicon.ico'`,
		`url(/assets/`, `url(assets/`,
		`url('/assets/`, `url('assets/`,
		`url("/assets/`, `url("assets/`,
	).Replace(content)
	if strings.Contains(content, "</head>") {
		return strings.Replace(content, "</head>", `<script src="bridge.js"></script></head>`, 1)
	}
	return `<script src="bridge.js"></script>` + content
}

// webViewBridge intentionally exposes a compatibility subset, never Wails or
// setup process privileges. The package metadata is JSON-quoted to keep a
// malformed manifest from becoming executable JavaScript.
func webViewBridge(entry robot.WebViewEntry) string {
	return `(function(){var listeners=[],lastAPIError='';function emit(value){listeners.slice().forEach(function(listener){try{listener(value)}catch(_){}})}function send(type,value){parent.postMessage({source:'alx-webview',type:type,value:value},'*')}function reportAPIError(status,message){var key=String(status||0)+'/'+String(message||'');if(lastAPIError===key)return;lastAPIError=key;send('api-error',{status:status||0,message:message||''});window.setTimeout(function(){lastAPIError=''},5000)}function responseError(response){response.clone().json().then(function(payload){reportAPIError(response.status,payload&&typeof payload.error==='string'?payload.error:'')}).catch(function(){reportAPIError(response.status,'')})}function isPluginAPI(input){try{var url=new URL(typeof input==='string'?input:input.url,location.href);return /\/api\//.test(url.pathname)}catch(_){return false}}var nativeFetch=window.fetch;window.fetch=function(input,init){return nativeFetch.apply(this,arguments).then(function(response){if(isPluginAPI(input)&&!response.ok)responseError(response);return response})};var NativeXHR=window.XMLHttpRequest;function TrackedXHR(){var xhr=new NativeXHR(),url='';var open=xhr.open;xhr.open=function(method,nextURL){url=nextURL;return open.apply(xhr,arguments)};xhr.addEventListener('loadend',function(){if(isPluginAPI(url)&&xhr.status>=400){var message='';try{var body=JSON.parse(xhr.responseText);message=typeof body.error==='string'?body.error:''}catch(_){}reportAPIError(xhr.status,message)}});return xhr}TrackedXHR.prototype=NativeXHR.prototype;window.XMLHttpRequest=TrackedXHR;function post(value){send('message',value);return nativeFetch('message',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({value:value})}).then(function(response){if(!response.ok)throw new Error('插件 desk 通信不可用')})}function pull(){nativeFetch('events').then(function(response){return response.ok?response.json():null}).then(function(payload){(payload&&payload.events||[]).forEach(emit)}).catch(function(){})}var api=Object.freeze({context:Object.freeze({package:` + strconv.Quote(entry.Package) + `,name:` + strconv.Quote(entry.Name) + `}),postMessage:post,onMessage:function(listener){if(typeof listener!=='function')return function(){};listeners.push(listener);return function(){listeners=listeners.filter(function(item){return item!==listener})}},request:function(path,options){if(typeof path!=='string'||!/^\.\/api\//.test(path))return Promise.reject(new Error('只允许请求插件 ./api/ 路径'));return window.fetch(path,options)}});window.__alxWebview=api;window.appDesktopAPI=Object.freeze({postMessage:api.postMessage,onMessage:api.onMessage,themeVariables:function(){return getComputedStyle(document.documentElement)},themeOn:function(listener){return api.onMessage(function(value){if(value&&value.type==='theme')listener(value.data)})}});window.addEventListener('message',function(event){var data=event.data;if(data&&data.source==='alx-parent'){emit(data.value)}});pull();window.setInterval(pull,800);send('ready',{package:api.context.package,name:api.context.name});})();`
}

// proxyRobotWebViewAPI connects a WebView's relative ./api/* requests to the
// selected robot application. The destination is never supplied by the
// browser: it is derived from the selected root's configured local app port.
// robotAppHandler reverse-proxies the robot's application service (the server
// on serverPort) so its launchpad and plugin pages render inside the alx page
// instead of relying on the old webview mechanism. All paths under /app/ map to
// http://127.0.0.1:<port>/..., preserving static assets and API routes.
//
// The robot root is carried in the path (a base64url token as the first path
// segment) rather than a query parameter, so every in-app navigation — links,
// relative assets, redirects and API calls — inherits the root automatically
// and never needs to re-send it. Legacy ?root= URLs are still accepted.
//
// The app is written against its own origin root, so its documents are adjusted
// on the way through: a <base href> is injected for relative assets/APIs, and
// root-relative links (/app/, /apps/x/) and redirects are re-prefixed with the
// proxy mount so navigation stays inside the iframe instead of escaping to the
// management page origin.
func (s *server) robotAppHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	const mount = "/api/v1/robot/app/"
	root, token := robotAppRootFromPath(r.URL.Path, mount)
	if root == "" {
		root = r.URL.Query().Get("root")
	}
	if root == "" {
		writeError(w, http.StatusBadRequest, "请选择机器人目录。")
		return
	}
	if token == "" {
		token = robotAppToken(root)
	}
	info, err := s.robots.AppPort(root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	target, err := url.Parse("http://127.0.0.1:" + strconv.Itoa(info.Port))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "应用地址无效。")
		return
	}
	// appPrefix is the mount plus the root token, e.g. /api/v1/robot/app/<token>/
	// Every rewritten link, base href and redirect targets this prefix so the
	// root travels inside the URL path.
	appPrefix := mount + token + "/"
	proxy := &httputil.ReverseProxy{
		Director: func(request *http.Request) {
			request.URL.Scheme = target.Scheme
			request.URL.Host = target.Host
			// Strip the mount and the root token, keeping the app's own path.
			rest := strings.TrimPrefix(r.URL.Path, mount)
			path := "/" + rest
			if rest == token || strings.HasPrefix(rest, token+"/") {
				path = strings.TrimPrefix(rest, token)
				if path == "" {
					path = "/"
				}
			}
			request.URL.Path = path
			request.URL.RawPath = ""
			request.Host = target.Host
		},
		ModifyResponse: func(response *http.Response) error {
			modifyRobotAppResponse(response, target, appPrefix, r.URL.Path, r)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			writeError(w, http.StatusBadGateway, "机器人应用尚未启动或无法连接。请在“运行”中启动开发模式后重试。")
		},
	}
	proxy.ServeHTTP(w, r)
}

// robotAppToken encodes a robot directory path for embedding in the proxy URL.
// It uses base64url without padding so the token is safe in a path segment.
func robotAppToken(root string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(root))
}

// robotAppRootFromPath extracts the root token from a proxied path's first
// segment, returning the decoded root and the raw token. The second return is
// empty when the path carries no valid token (legacy query-form URLs). Only
// absolute paths are accepted, so an arbitrary app route like "apps/demo"
// (whose first segment is coincidentally valid base64url) is never misread as
// a robot root.
func robotAppRootFromPath(path, mount string) (root, token string) {
	rest := strings.TrimPrefix(path, mount)
	segment, _, _ := strings.Cut(rest, "/")
	if segment == "" {
		return "", ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return "", ""
	}
	if len(decoded) == 0 || decoded[0] != '/' {
		return "", ""
	}
	return string(decoded), segment
}

// modifyRobotAppResponse rewrites a proxied app response so absolute paths stay
// inside the proxy mount. Redirects are re-prefixed and HTML documents get a
// <base href> plus rewritten root-relative href/src/action references. Unmatched
// page navigations (upstream 404) are sent back to the launchpad instead of
// rendering stray content, so the app frame never nests the management page.
func modifyRobotAppResponse(response *http.Response, target *url.URL, appPrefix, requestPath string, request *http.Request) {
	if isRobotAppNavigation(request) && requestPath != appPrefix && response.StatusCode == http.StatusNotFound {
		response.Header.Set("Location", appPrefix)
		response.StatusCode = http.StatusFound
		response.Body = io.NopCloser(bytes.NewReader(nil))
		response.ContentLength = 0
		response.Header.Del("Content-Type")
		response.Header.Set("Content-Length", "0")
		return
	}
	if location := response.Header.Get("Location"); location != "" {
		if rewritten := rewriteRobotAppLocation(location, target, appPrefix); rewritten != "" {
			response.Header.Set("Location", rewritten)
		}
	}
	// Only document bodies need rewriting; assets and API payloads pass through.
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(contentType, "text/html") {
		return
	}
	// Compressed bodies cannot be safely patched; leave them untouched.
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return
	}
	_ = response.Body.Close()
	rewritten := rewriteRobotAppHTML(body, appPrefix, requestPath)
	response.Body = io.NopCloser(bytes.NewReader(rewritten))
	response.ContentLength = int64(len(rewritten))
	response.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
}

// isRobotAppNavigation reports whether a proxied request is a full-page HTML
// navigation (as opposed to a script/style/fetch request), so unmatched-route
// redirects never hijack asset or API responses.
func isRobotAppNavigation(request *http.Request) bool {
	return strings.Contains(strings.ToLower(request.Header.Get("Accept")), "text/html")
}

// rewriteRobotAppLocation re-prefixes a root-relative (or target-host) redirect
// Location so the browser follows it through the proxy instead of escaping to
// the management page origin. Absolute external URLs are left alone.
func rewriteRobotAppLocation(location string, target *url.URL, prefix string) string {
	if strings.HasPrefix(location, target.Scheme+"://"+target.Host) {
		if parsed, err := url.Parse(location); err == nil {
			return prefix + strings.TrimPrefix(parsed.RequestURI(), "/")
		}
		return ""
	}
	if strings.HasPrefix(location, "/") && !strings.HasPrefix(location, "//") {
		return prefix + strings.TrimPrefix(location, "/")
	}
	return ""
}

// rewriteRobotAppHTML injects a <base href> for the current document directory
// (so relative assets like ./assets/... and ./api/... resolve within the mount)
// and re-prefixes root-relative href/src/action references (like the launchpad's
// /app/ links) which ignore <base>.
func rewriteRobotAppHTML(body []byte, prefix, requestPath string) []byte {
	document := string(body)
	// Root-relative absolute paths (/app/, /apps/x/, /favicon.ico) bypass <base>.
	// Protocol-relative (//host) and scheme URLs must stay untouched. This runs
	// first so the injected <base href> (itself a root-relative path) is not
	// re-prefixed.
	document = rewriteRootRelativeAttr(document, prefix)
	injections := baseHrefFor(requestPath) + robotAppScrollbarStyle
	if head := regexp.MustCompile(`(?i)<head[^>]*>`).FindString(document); head != "" {
		document = strings.Replace(document, head, head+injections, 1)
	} else if htmlTag := regexp.MustCompile(`(?i)<html[^>]*>`).FindString(document); htmlTag != "" {
		document = strings.Replace(document, htmlTag, htmlTag+"<head>"+injections+"</head>", 1)
	} else {
		document = injections + document
	}
	return []byte(document)
}

// robotAppScrollbarStyle hides the app document's scrollbars (Chrome/Edge/Safari
// via ::-webkit-scrollbar, Firefox via scrollbar-width) so the embedded app does
// not show a second scrollbar beside the management page's own. Scrolling itself
// is preserved.
const robotAppScrollbarStyle = `<style>html,body{scrollbar-width:none;-ms-overflow-style:none}html::-webkit-scrollbar,body::-webkit-scrollbar{width:0;height:0;display:none}</style>`

// baseHrefFor returns the <base href> pointing at the current document's
// directory within the proxy mount, e.g. /api/v1/robot/app/apps/x/.
func baseHrefFor(requestPath string) string {
	directory := requestPath
	if index := strings.LastIndex(directory, "/"); index >= 0 {
		directory = directory[:index+1]
	}
	return `<base href="` + html.EscapeString(directory) + `">`
}

// rewriteRootRelativeAttr prefixes root-relative values of href/src/action
// attributes (and their single-quoted variants) with the proxy mount.
// Protocol-relative (//host) and scheme URLs are left untouched.
func rewriteRootRelativeAttr(document, prefix string) string {
	for _, attr := range []string{"href", "src", "action"} {
		for _, quote := range []string{`"`, `'`} {
			pattern := regexp.MustCompile(`(?i)(\b` + attr + `=` + quote + `)(/[^` + quote + `]*)` + quote)
			document = pattern.ReplaceAllStringFunc(document, func(match string) string {
				parts := pattern.FindStringSubmatch(match)
				if strings.HasPrefix(parts[2], "//") {
					return match
				}
				return parts[1] + prefix + strings.TrimPrefix(parts[2], "/") + quote
			})
		}
	}
	return document
}

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

func (s *server) robotWebViewMessageHandler(w http.ResponseWriter, r *http.Request, root, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "插件消息仅支持发送。")
		return
	}
	entry, err := s.robots.WebViewEntry(root, id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var input struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256*1024)).Decode(&input); err != nil || len(input.Value) == 0 {
		writeError(w, http.StatusBadRequest, "插件消息内容无效。")
		return
	}
	runtime, err := s.ensureRobotWebViewRuntime(root)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	payload, _ := json.Marshal(map[string]json.RawMessage{"name": json.RawMessage(strconv.Quote(entry.Package)), "value": input.Value})
	message, _ := json.Marshal(map[string]any{"type": "webview-post-message", "data": string(payload), "__STDIN_JSON_DATA": true})
	if _, err := runtime.stdin.Write(append(message, '\n')); err != nil {
		writeError(w, http.StatusBadGateway, "插件 desk 进程无法接收消息。")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}

// robotWebViewEventsHandler is deliberately a short polling endpoint. Unlike a
// permanent SSE stream, it keeps hidden tabs from holding resources while the
// browser still receives every queued message from its directory's desk IPC.
func (s *server) robotWebViewEventsHandler(w http.ResponseWriter, r *http.Request, root, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "插件消息仅支持读取。")
		return
	}
	if _, err := s.robots.WebViewEntry(root, id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.mu.Lock()
	runtime := s.webviewRuntimes[root]
	events := []json.RawMessage(nil)
	if runtime != nil {
		events = append(events, runtime.events[id]...)
		delete(runtime.events, id)
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *server) ensureRobotWebViewRuntime(root string) (*webViewRuntime, error) {
	s.mu.RLock()
	runtime := s.webviewRuntimes[root]
	s.mu.RUnlock()
	if runtime != nil {
		return runtime, nil
	}
	// The caller already resolved a registered WebView entry for this root.
	// Convert only that validated root to an absolute process working directory.
	project, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	scriptPath := filepath.Join(project, "alemonjs", "desktop.js")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(scriptPath, []byte(defaultWebViewDesktopScript), 0644); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, fmt.Errorf("插件 WebView 通信需要 Node.js，请先在环境管理中安装")
	}
	command := exec.Command(node, scriptPath)
	command.Dir = project
	robot.HideWindow(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("无法启动插件 desk 进程：%w", err)
	}
	runtime = &webViewRuntime{command: command, stdin: stdin, events: map[string][]json.RawMessage{}}
	s.mu.Lock()
	if existing := s.webviewRuntimes[root]; existing != nil {
		s.mu.Unlock()
		_ = command.Process.Kill()
		go func() { _ = command.Wait() }()
		return existing, nil
	}
	s.webviewRuntimes[root] = runtime
	s.mu.Unlock()
	go s.watchRobotWebViewRuntime(root, runtime, stdout, stderr)
	return runtime, nil
}

func (s *server) watchRobotWebViewRuntime(root string, runtime *webViewRuntime, stdout, stderr io.Reader) {
	read := func(stream io.Reader) {
		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			s.queueRobotWebViewEvent(root, scanner.Bytes())
		}
	}
	go read(stderr)
	read(stdout)
	_ = runtime.command.Wait()
	s.mu.Lock()
	if s.webviewRuntimes[root] == runtime {
		delete(s.webviewRuntimes, root)
	}
	s.mu.Unlock()
}

func (s *server) queueRobotWebViewEvent(root string, line []byte) {
	var envelope struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(line, &envelope) != nil || !strings.HasPrefix(envelope.Type, "webview-") {
		return
	}
	var target struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	if json.Unmarshal(envelope.Data, &target) != nil || target.Name == "" {
		return
	}
	entries, err := s.robots.WebViews(root)
	if err != nil {
		return
	}
	event, _ := json.Marshal(map[string]json.RawMessage{"type": json.RawMessage(strconv.Quote(strings.TrimPrefix(envelope.Type, "webview-on-"))), "data": target.Value})
	s.mu.Lock()
	defer s.mu.Unlock()
	runtime := s.webviewRuntimes[root]
	if runtime == nil {
		return
	}
	for _, entry := range entries {
		if entry.Package == target.Name {
			runtime.events[entry.ID] = append(runtime.events[entry.ID], event)
		}
	}
}

const defaultWebViewDesktopScript = `import { events } from '@alemonjs/process'
const send = data => process.stdout.write(JSON.stringify({ type: data.type, data: data.data, from: 'nodejs', __STDIN_JSON_DATA: true }) + '\n')
global.wsprocess = global.wsprocess || {}; global.wsprocess.send = send
process.stdin.on('data', raw => { try { const data = JSON.parse(raw.toString().trim()); if (data?.__STDIN_JSON_DATA && data.type) events[data.type]?.(JSON.parse(data.data)) } catch {} })
`

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
		view := r.URL.Query().Get("view")
		if view == "" {
			view = "commit"
		}
		status, err := robot.GitWorkspaceView(r.URL.Query().Get("root"), view)
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
		Value   string `json:"value"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Git 操作内容无法识别。")
		return
	}
	result, err := robot.GitWorkspaceAction(input.Root, input.Action, input.Value, input.Message)
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
		Depth       int    `json:"depth"`
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
	if err := robot.ValidateCloneRepository(input.Repository); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	target, err := robot.CloneDestination(destination, input.Repository, input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if target.Exists {
		writeError(w, http.StatusConflict, "目标目录已存在。")
		return
	}
	created := operationTask{ID: "clone-" + time.Now().Format("20060102150405.000000000"), Root: destination, Path: target.Path, Action: "git-clone", Status: "running", Output: "正在启动 Git…", Progress: 0, CreatedAt: time.Now()}
	s.addOperation(created)
	go func() {
		result, err := robot.CloneRepositoryWithProgress(destination, input.Repository, input.Branch, input.Name, input.Mirror, input.Depth, func(progress robot.CloneProgress) {
			s.updateOperation(created.ID, progress.Percent, progress.Detail, "", false)
		})
		if err != nil {
			s.updateOperation(created.ID, 100, result.Output, err.Error(), true)
			return
		}
		s.updateOperation(created.ID, 100, result.Output, "", true)
	}()
	writeJSON(w, http.StatusAccepted, created)
}

func (s *server) updateOperation(id string, progress int, output, failure string, finished bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.operations {
		if s.operations[index].ID != id {
			continue
		}
		s.operations[index].Progress = progress
		if output != "" {
			s.operations[index].Output = output
		}
		if finished {
			now := time.Now()
			s.operations[index].FinishedAt = &now
			s.operations[index].Status = "completed"
			if failure != "" {
				s.operations[index].Status = "failed"
				s.operations[index].Error = failure
			}
		}
		return
	}
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

func (s *server) robotGitCloneBranchesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	branches, defaultBranch, err := robot.CloneBranches(r.URL.Query().Get("repository"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"branches": branches, "defaultBranch": defaultBranch})
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

func (s *server) listGoals(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, goals)
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
		// 开发模式下前端 Vite（localhost:5173）的 SSE 流式请求绕过代理直连
		// 后端，需要跨域许可。仅对 5173 本地来源放行，其余一律不加。
		origin := c.GetHeader("Origin")
		if origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type")
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
		}
		c.Header("X-Content-Type-Options", "nosniff")
		// Registered robot WebViews, the in-page application service and setup
		// plugin pages are embedded in frames, so X-Frame-Options cannot be
		// SAMEORIGIN for them. Their responses carry their own CSP.
		if !strings.HasPrefix(c.Request.URL.Path, "/api/v1/robot/webview/") &&
			!strings.HasPrefix(c.Request.URL.Path, "/api/v1/robot/app/") &&
			!strings.HasPrefix(c.Request.URL.Path, "/api/v1/setup/plugins/web/") {
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
		if !strings.HasPrefix(path, "/api/v1/") || path == "/api/v1/auth/status" || path == "/api/v1/auth/setup" || path == "/api/v1/auth/login" || path == "/api/v1/auth/logout" || strings.HasPrefix(path, "/api/v1/robot/webview/") || strings.HasPrefix(path, "/api/v1/robot/app/") {
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
		if query := c.Request.URL.RawQuery; query != "" {
			// The browser always sends query params (root, file, page, …); echo
			// them so a front-end action is traceable from the console.
			log.Printf("[GIN %06d] 参数 %s", id, query)
		}
		if body := s.loggableRequestBody(c); body != "" {
			log.Printf("[GIN %06d] 请求 %s", id, body)
		}
		c.Next()
		status := c.Writer.Status()
		label := "完成"
		if status >= http.StatusBadRequest {
			label = "失败"
		}
		log.Printf("[GIN %06d] %s status=%d duration=%s response=%dB", id, label, status, time.Since(started).Round(time.Millisecond), c.Writer.Size())
	}
}

// loggableRequestBody reads a bounded JSON body for logging and restores the
// stream so handlers still decode it. Sensitive fields (tokens, passwords) are
// redacted; the body is capped to keep logs readable.
func (s *server) loggableRequestBody(c *gin.Context) string {
	if c.Request.Body == nil || c.Request.Body == http.NoBody {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 16*1024))
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		// Not JSON; log a trimmed prefix.
		text := strings.TrimSpace(string(raw))
		if len(text) > 200 {
			text = text[:200] + "…"
		}
		return text
	}
	for _, key := range []string{"token", "password", "confirmation", "content", "message", "values"} {
		if _, ok := fields[key]; ok {
			fields[key] = "[REDACTED]"
		}
	}
	encoded, _ := json.Marshal(fields)
	return string(encoded)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
