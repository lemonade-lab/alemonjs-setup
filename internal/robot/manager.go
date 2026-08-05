// Package robot provides guarded local project management operations.
package robot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"alemonjs-setup/internal/system"
)

type Manager struct{}
type Result struct {
	Output string `json:"output"`
	Path   string `json:"path"`
}

// Validate confirms that a saved robot directory still exists and is an
// eligible Node.js project. It is intentionally side-effect free.
func (Manager) Validate(root string) (Result, error) {
	path, err := projectPath(root)
	if err != nil {
		return Result{}, err
	}
	return Result{Path: path, Output: "机器人目录可用。"}, nil
}

// LocalPackage is a bundled plugin found in the robot's packages directory.
type LocalPackage struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
	Valid       bool   `json:"valid"`
}

const maxMCPFileSize = 1024 * 1024

// ListProjectFiles returns source and configuration files that an MCP client
// may inspect. Dependency trees, Git metadata, secrets, and symlinks are
// intentionally excluded even though they may live under the project root.
func (Manager) ListProjectFiles(root string) ([]string, error) {
	path, err := projectPath(root)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	err = filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == path {
			return nil
		}
		relative, err := filepath.Rel(path, current)
		if err != nil {
			return err
		}
		if blockedProjectPath(relative) || entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		files = append(files, filepath.ToSlash(relative))
		if len(files) > 1000 {
			return errors.New("项目文件过多；请缩小项目目录后重试")
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("读取项目文件列表失败：%w", err)
	}
	sort.Strings(files)
	return files, nil
}

// ReadProjectFile reads a non-sensitive, regular file below a managed robot
// project. It is separate from Read so the UI's narrow configuration-file
// contract remains unchanged.
func (Manager) ReadProjectFile(root, name string) (Result, error) {
	path, err := managedProjectFile(root, name)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, fmt.Errorf("读取失败：%w", err)
	}
	if info.Size() > maxMCPFileSize {
		return Result{}, errors.New("文件超过 1 MiB，MCP 不会读取")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("读取失败：%w", err)
	}
	return Result{Path: path, Output: string(data)}, nil
}

// WriteProjectFile writes a non-sensitive source/configuration file within a
// managed project. It does not create directories and rejects symlinks.
func (Manager) WriteProjectFile(root, name, content string) (Result, error) {
	if len(content) > maxMCPFileSize {
		return Result{}, errors.New("文件内容超过 1 MiB，MCP 不会写入")
	}
	path, err := managedProjectFile(root, name)
	if err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return Result{}, errors.New("目标目录不存在；MCP 不会自动创建目录")
	}
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return Result{}, errors.New("只能写入普通项目文件")
	} else if err != nil && !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("无法检查目标文件：%w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return Result{}, fmt.Errorf("保存失败：%w", err)
	}
	return Result{Path: path, Output: "已保存。"}, nil
}

func (Manager) LocalPackages(root string) ([]LocalPackage, error) {
	path, err := projectPath(root)
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(path, "packages")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []LocalPackage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("无法读取 packages 目录：%w", err)
	}
	items := make([]LocalPackage, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			item := LocalPackage{Name: entry.Name(), Path: filepath.Join(directory, entry.Name())}
			data, readErr := os.ReadFile(filepath.Join(item.Path, "package.json"))
			if readErr == nil {
				var manifest struct {
					Name        string `json:"name"`
					Version     string `json:"version"`
					Description string `json:"description"`
				}
				if json.Unmarshal(data, &manifest) == nil && manifest.Name != "" {
					item.Name, item.Version, item.Description, item.Valid = manifest.Name, manifest.Version, manifest.Description, true
				}
			}
			items = append(items, item)
		}
	}
	return items, nil
}

// Console returns a fixed, read-only project snapshot. There is deliberately
// no command argument: the web UI can present terminal-like context without
// exposing a browser shell that accepts arbitrary input.
func (Manager) Console(root string) (Result, error) {
	path, err := projectPath(root)
	if err != nil {
		return Result{}, err
	}
	lines := []string{"$ pwd", path}
	var manifest struct {
		Name    string            `json:"name"`
		Version string            `json:"version"`
		Scripts map[string]string `json:"scripts"`
	}
	if data, readErr := os.ReadFile(filepath.Join(path, "package.json")); readErr == nil && json.Unmarshal(data, &manifest) == nil {
		lines = append(lines, "", "$ package.json", fmt.Sprintf("%s@%s", manifest.Name, manifest.Version))
		keys := make([]string, 0, len(manifest.Scripts))
		for key := range manifest.Scripts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			lines = append(lines, "", "$ scripts")
			for _, key := range keys {
				lines = append(lines, key+" · "+manifest.Scripts[key])
			}
		}
	}
	lines = append(lines, "", "$ git status --short")
	if output, gitErr := run(path, "git", "status", "--short"); gitErr == nil {
		if output == "" {
			lines = append(lines, "工作区干净")
		} else {
			lines = append(lines, output)
		}
	} else {
		lines = append(lines, "当前目录尚未初始化 Git，或 Git 不可用。")
	}
	lines = append(lines, "", "$ node --version")
	if output, nodeErr := run(path, "node", "--version"); nodeErr == nil {
		lines = append(lines, output)
	} else {
		lines = append(lines, "未检测到 Node.js。")
	}
	return Result{Path: path, Output: strings.Join(lines, "\n")}, nil
}

// DevelopmentCommand prepares the project's declared development command for
// the web server to supervise. Its stdout and stderr stay attached to the
// operation record, so the UI can show progress without exposing a shell.
func (Manager) DevelopmentCommand(root string) (*exec.Cmd, error) {
	if err := project(root); err != nil {
		return nil, err
	}
	if err := fixLegacyLvyScript(root); err != nil {
		return nil, err
	}
	manager := projectPackageManager(root)
	command := exec.Command(manager, "run", "dev")
	command.Dir = root
	return command, nil
}

type privilegedRequest struct {
	Operation   string `json:"operation"`
	Root        string `json:"root"`
	File        string `json:"file,omitempty"`
	Content     string `json:"content,omitempty"`
	Action      string `json:"action,omitempty"`
	Message     string `json:"message,omitempty"`
	PackageName string `json:"packageName,omitempty"`
	Version     string `json:"version,omitempty"`
	Tag         string `json:"tag,omitempty"`
	Token       string `json:"token,omitempty"`
	Confirmed   bool   `json:"confirmed,omitempty"`
}

type privilegedResponse struct {
	Result Result `json:"result"`
	Error  string `json:"error,omitempty"`
}

func (m Manager) Read(root, name string) (Result, error) {
	path, err := file(root, name)
	if err != nil {
		if permissionError(err) {
			return m.withPrivileges(privilegedRequest{Operation: "read", Root: root, File: name})
		}
		return Result{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// These two files are editable project configuration. A new robot
		// project legitimately may not have created either one yet; present an
		// empty document so the editor can create it on save.
		if errors.Is(err, os.ErrNotExist) && (name == "alemon.config.yaml" || name == ".npmrc") {
			return Result{Path: path, Output: ""}, nil
		}
		if permissionError(err) {
			return m.withPrivileges(privilegedRequest{Operation: "read", Root: root, File: name})
		}
		return Result{}, fmt.Errorf("读取失败：%w", err)
	}
	return Result{Path: path, Output: string(data)}, nil
}
func (m Manager) Write(root, name, content string) (Result, error) {
	path, err := file(root, name)
	if err != nil {
		if permissionError(err) {
			return m.withPrivileges(privilegedRequest{Operation: "write", Root: root, File: name, Content: content})
		}
		return Result{}, err
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		if !os.IsPermission(err) {
			return Result{}, fmt.Errorf("保存 %s 失败：%w", filepath.Base(path), err)
		}
		if elevatedErr := system.WriteFileWithPrivileges(path, []byte(content)); elevatedErr != nil {
			return Result{}, fmt.Errorf("当前用户没有写入 %s 的权限。请先允许系统权限提示；若这是服务器目录，请把项目交给当前用户管理后重试。详情：%w", filepath.Base(path), elevatedErr)
		}
	}
	return Result{Path: path, Output: "已保存。"}, nil
}
func (m Manager) Run(root, action, message, packageName, version, tag, token string, confirmed bool) (Result, error) {
	request := privilegedRequest{Operation: "run", Root: root, Action: action, Message: message, PackageName: packageName, Version: version, Tag: tag, Token: token, Confirmed: confirmed}
	if action == "git-release" {
		if _, err := workspacePath(root); permissionError(err) {
			return m.withPrivileges(request)
		}
		// message carries the source commit selected in the Git publishing card.
		// Keeping it on the existing task payload also preserves privileged runs.
		return GitPublish(root, version, message, confirmed)
	}
	if err := project(root); err != nil {
		if permissionError(err) {
			return m.withPrivileges(request)
		}
		return Result{}, err
	}
	manager := "npm"
	if _, err := os.Stat(filepath.Join(root, "yarn.lock")); err == nil {
		manager = "yarn"
	}
	var name string
	var args []string
	switch action {
	case "dependency-status":
		checks := []string{}
		if _, err := os.Stat(filepath.Join(root, "node_modules")); err == nil {
			checks = append(checks, "已发现 node_modules，依赖目录已就绪。")
		} else {
			checks = append(checks, "未发现 node_modules，需要安装依赖。")
		}
		if _, err := os.Stat(filepath.Join(root, "yarn.lock")); err == nil {
			checks = append(checks, "检测到 yarn.lock，将使用 Yarn 管理依赖。")
		} else if _, err := os.Stat(filepath.Join(root, "package-lock.json")); err == nil {
			checks = append(checks, "检测到 package-lock.json，将使用 npm 管理依赖。")
		} else {
			checks = append(checks, "未检测到锁定文件，安装时会根据 package.json 生成依赖状态。")
		}
		return Result{Path: root, Output: strings.Join(checks, "\n")}, nil
	case "install":
		name, args = manager, []string{"install"}
	case "build":
		if err := fixLegacyLvyScript(root); err != nil {
			return Result{}, err
		}
		name, args = manager, []string{"run", "build"}
	case "npm-publish":
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(tag) {
			return Result{}, errors.New("npm 标签格式无效")
		}
		// message is the explicitly selected source commit, shared with the
		// Git release flow. NPM receives a tarball made from that revision.
		return (Manager{}).NPMPublish(root, message, tag, token)
	case "npm-version":
		if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(version) {
			return Result{}, errors.New("版本号应为 1.2.3")
		}
		name, args = "npm", []string{"version", version, "--no-git-tag-version"}
	case "dev":
		if err := fixLegacyLvyScript(root); err != nil {
			return Result{}, err
		}
		name, args = manager, []string{"run", "dev"}
	case "commit":
		if strings.TrimSpace(message) == "" {
			return Result{}, errors.New("请填写本次提交说明")
		}
		name, args = "git", []string{"add", "."}
		if _, err := run(root, name, args...); err != nil {
			return Result{}, err
		}
		name, args = "git", []string{"commit", "-m", message}
	case "pm2":
		if _, err := run(root, manager, "run", "build"); err != nil {
			return Result{}, err
		}
		name, args = "npx", []string{"pm2", "startOrRestart", "pm2.config.cjs"}
	case "pm2-stop":
		name, args = "npx", []string{"pm2", "stop", "pm2.config.cjs"}
	case "pm2-status":
		name, args = "npx", []string{"pm2", "list"}
	case "install-package":
		if !allowedInstallPackage(packageName) {
			return Result{}, errors.New("不支持的 AlemonJS 包")
		}
		return installLocalPackage(root, packageName)
	case "uninstall-package":
		if !allowedPackage(packageName) {
			return Result{}, errors.New("不支持的 AlemonJS 包")
		}
		return removeLocalPackage(root, packageName)
	case "git-init":
		if _, err := run(root, "git", "init"); err != nil {
			return Result{}, err
		}
		name, args = "git", []string{"branch", "-M", "main"}
	default:
		return Result{}, errors.New("未知的机器人操作")
	}
	output, err := run(root, name, args...)
	return Result{Path: root, Output: output}, err
}

func allowedInstallPackage(name string) bool {
	if allowedPackage(name) {
		return true
	}
	if at := strings.LastIndex(name, "@"); at > strings.LastIndex(name, "/") {
		return allowedPackage(name[:at])
	}
	return false
}

// ExecutePrivilegedRequest is called only by the same executable after the
// operating system has authenticated the one requested action.
func ExecutePrivilegedRequest(requestPath, resultPath string) error {
	data, err := os.ReadFile(requestPath)
	if err != nil {
		return err
	}
	var request privilegedRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return err
	}
	manager := Manager{}
	var result Result
	switch request.Operation {
	case "read":
		result, err = manager.Read(request.Root, request.File)
	case "write":
		result, err = manager.Write(request.Root, request.File, request.Content)
	case "run":
		result, err = manager.Run(request.Root, request.Action, request.Message, request.PackageName, request.Version, request.Tag, request.Token, request.Confirmed)
	default:
		err = errors.New("未知的权限操作")
	}
	response := privilegedResponse{Result: result}
	if err != nil {
		response.Error = err.Error()
	}
	data, err = json.Marshal(response)
	if err != nil {
		return err
	}
	return os.WriteFile(resultPath, data, 0666)
}

func (Manager) withPrivileges(request privilegedRequest) (Result, error) {
	executable, err := os.Executable()
	if err != nil {
		return Result{}, fmt.Errorf("无法申请系统权限：%w", err)
	}
	requestFile, err := os.CreateTemp("", "albs-robot-request-*.json")
	if err != nil {
		return Result{}, fmt.Errorf("无法准备权限申请：%w", err)
	}
	requestPath := requestFile.Name()
	defer os.Remove(requestPath)
	resultFile, err := os.CreateTemp("", "albs-robot-result-*.json")
	if err != nil {
		return Result{}, fmt.Errorf("无法准备权限申请：%w", err)
	}
	resultPath := resultFile.Name()
	defer os.Remove(resultPath)
	if err := resultFile.Chmod(0666); err != nil {
		return Result{}, fmt.Errorf("无法准备权限申请：%w", err)
	}
	if err := resultFile.Close(); err != nil {
		return Result{}, fmt.Errorf("无法准备权限申请：%w", err)
	}
	data, err := json.Marshal(request)
	if err != nil {
		return Result{}, fmt.Errorf("无法准备权限申请：%w", err)
	}
	if _, err := requestFile.Write(data); err != nil {
		return Result{}, fmt.Errorf("无法准备权限申请：%w", err)
	}
	if err := requestFile.Close(); err != nil {
		return Result{}, fmt.Errorf("无法准备权限申请：%w", err)
	}
	if _, err := system.RunWithPrivileges("", nil, executable, "--privileged-robot", requestPath, resultPath); err != nil {
		return Result{}, err
	}
	data, err = os.ReadFile(resultPath)
	if err != nil {
		return Result{}, fmt.Errorf("权限操作未返回结果：%w", err)
	}
	var response privilegedResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return Result{}, fmt.Errorf("权限操作返回格式无法识别：%w", err)
	}
	if response.Error != "" {
		return response.Result, errors.New(response.Error)
	}
	return response.Result, nil
}
func allowedPackage(name string) bool {
	if strings.HasPrefix(name, "git+https://github.com/") || strings.HasPrefix(name, "git+https://gitee.com/") {
		return true
	}
	for _, item := range []string{"alemonjs", "@alemonjs/bubble", "@alemonjs/db", "@alemonjs/discord", "@alemonjs/onebot", "@alemonjs/qq-bot", "@alemonjs/kook", "@alemonjs/telegram"} {
		if name == item {
			return true
		}
	}
	return false
}
func project(root string) error {
	_, err := projectPath(root)
	return err
}

func projectPath(root string) (string, error) {
	if root == "." {
		current, err := os.Getwd()
		if err != nil {
			return "", errors.New("无法读取当前运行目录")
		}
		root = current
	}
	if !filepath.IsAbs(root) {
		return "", errors.New("请选择完整的机器人文件夹路径")
	}
	info, err := os.Stat(root)
	if err != nil {
		if permissionError(err) {
			return "", fmt.Errorf("无法访问机器人文件夹：%w", err)
		}
		return "", errors.New("机器人文件夹不存在")
	}
	if !info.IsDir() {
		return "", errors.New("机器人文件夹不存在")
	}
	if _, err := os.Stat(filepath.Join(root, "package.json")); err != nil {
		if permissionError(err) {
			return "", fmt.Errorf("无法读取机器人 package.json：%w", err)
		}
		return "", errors.New("该文件夹不是可管理的 Node.js 机器人项目（缺少 package.json）")
	}
	return root, nil
}

func managedProjectFile(root, name string) (string, error) {
	projectRoot, err := projectPath(root)
	if err != nil {
		return "", err
	}
	relative := filepath.Clean(name)
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || blockedProjectPath(relative) {
		return "", errors.New("不允许访问该项目文件")
	}
	target := filepath.Join(projectRoot, relative)
	directory := filepath.Dir(target)
	directoryRelative, err := filepath.Rel(projectRoot, directory)
	if err != nil {
		return "", errors.New("目标文件不在机器人项目中")
	}
	current := projectRoot
	for _, part := range strings.Split(filepath.Clean(directoryRelative), string(filepath.Separator)) {
		if part != "." {
			current = filepath.Join(current, part)
		}
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("无法检查项目路径：%w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("不允许通过符号链接访问项目文件")
		}
	}
	return target, nil
}

func blockedProjectPath(name string) bool {
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		lower := strings.ToLower(part)
		if lower == ".git" || lower == "node_modules" || lower == ".npmrc" || lower == ".env" || strings.HasPrefix(lower, ".env.") || strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") || strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx") {
			return true
		}
	}
	return false
}

func permissionError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return os.IsPermission(err) || strings.Contains(text, "eacces") || strings.Contains(text, "permission denied") || strings.Contains(text, "access is denied")
}
func file(root, name string) (string, error) {
	if err := project(root); err != nil {
		return "", err
	}
	if name != ".npmrc" && name != "alemon.config.yaml" && name != "README.md" {
		return "", errors.New("不支持的文件")
	}
	return filepath.Join(root, name), nil
}

// fixLegacyLvyScript upgrades templates created before lvy was called directly.
func fixLegacyLvyScript(root string) error {
	path := filepath.Join(root, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("无法读取 package.json：%w", err)
	}
	fixed := strings.ReplaceAll(string(data), "npx lvy ", "lvy ")
	if fixed == string(data) {
		return nil
	}
	if err := os.WriteFile(path, []byte(fixed), 0644); err != nil {
		return fmt.Errorf("无法更新开发脚本：%w", err)
	}
	return nil
}
func run(root, name string, args ...string) (string, error) {
	return runWithEnv(root, nil, name, args...)
}

func runWithEnv(root string, values map[string]string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	cmd.Env = os.Environ()
	for key, value := range values {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if os.IsPermission(err) || strings.Contains(strings.ToLower(text), "eacces") || strings.Contains(strings.ToLower(text), "permission denied") {
			elevated, elevatedErr := system.RunWithPrivileges(root, values, name, args...)
			if elevatedErr == nil {
				return elevated, nil
			}
			if elevated != "" {
				text = elevated
			}
			return text, fmt.Errorf("%s：当前用户权限不足，系统权限申请未完成：%w", strings.Join(append([]string{name}, args...), " "), elevatedErr)
		}
		return text, fmt.Errorf("%s：%w", text, err)
	}
	return text, nil
}
