// Package robot provides guarded local project management operations.
package robot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"alemonx/internal/catalog"
	"alemonx/internal/system"
)

type Manager struct{}
type Result struct {
	Output string `json:"output"`
	Path   string `json:"path"`
}

// PM2LogPage is a read-only slice of PM2 output. Page 1 is always the newest
// output, so users see the current problem before choosing to inspect history.
type PM2LogPage struct {
	Output   string `json:"output"`
	Page     int    `json:"page"`
	HasOlder bool   `json:"hasOlder"`
}

// PM2Status describes the PM2 process associated with one robot directory.
// It deliberately matches by PM2's working directory so unrelated services
// managed by the same local PM2 daemon never affect this project's controls.
type PM2Status struct {
	Configured bool   `json:"configured"`
	Managed    bool   `json:"managed"`
	Running    bool   `json:"running"`
	Status     string `json:"status,omitempty"`
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

// LocalPackageVersions identifies one authoritative version source for a
// backpack entry. A checked-out Git package always wins over npm metadata,
// matching the framework's source-first plugin convention.
type LocalPackageVersions struct {
	Source   string   `json:"source"`
	Current  string   `json:"current"`
	Latest   string   `json:"latest,omitempty"`
	Versions []string `json:"versions"`
}

func (m Manager) LocalPackageVersions(root, packageName string) (LocalPackageVersions, error) {
	items, err := m.LocalPackages(root)
	if err != nil {
		return LocalPackageVersions{}, err
	}
	for _, item := range items {
		if item.Name != packageName || !item.Valid {
			continue
		}
		if output, gitErr := gitRun(item.Path, "rev-parse", "--is-inside-work-tree"); gitErr == nil && strings.TrimSpace(output) == "true" {
			current, _ := gitRun(item.Path, "describe", "--tags", "--always", "--dirty")
			versions := packageGitVersions(item.Path)
			latest := ""
			if len(versions) > 0 {
				latest = versions[0]
			}
			return LocalPackageVersions{Source: "git", Current: strings.TrimSpace(current), Latest: latest, Versions: versions}, nil
		}
		versions, loadErr := catalog.LoadPackageVersions(item.Name)
		if loadErr != nil {
			return LocalPackageVersions{}, loadErr
		}
		return LocalPackageVersions{Source: "npm", Current: item.Version, Latest: versions.Latest, Versions: versions.Versions}, nil
	}
	return LocalPackageVersions{}, errors.New("背包中没有这个本地插件包")
}

// packageGitVersions enumerates published tags from origin without changing
// the checkout. A shallow clone often has no local tags, so only reading
// `git tag` would incorrectly make the version selector look empty.
func packageGitVersions(root string) []string {
	all := map[string]bool{}
	for _, tag := range gitLines(root, "tag", "--list", "v*", "--sort=-v:refname") {
		if gitVersionPattern.MatchString(tag) {
			all[tag] = true
		}
	}
	if remote, err := gitRun(root, "ls-remote", "--tags", "--refs", "origin"); err == nil {
		for _, line := range strings.Split(remote, "\n") {
			parts := strings.Fields(line)
			if len(parts) != 2 {
				continue
			}
			tag := strings.TrimPrefix(parts[1], "refs/tags/")
			if gitVersionPattern.MatchString(tag) {
				all[tag] = true
			}
		}
	}
	versions := make([]string, 0, len(all))
	for tag := range all {
		versions = append(versions, tag)
	}
	sort.Slice(versions, func(i, j int) bool { return newerGitTag(versions[i], versions[j]) })
	return versions
}

func newerGitTag(left, right string) bool {
	leftParts := strings.Split(strings.TrimPrefix(left, "v"), ".")
	rightParts := strings.Split(strings.TrimPrefix(right, "v"), ".")
	for index := 0; index < 3; index++ {
		leftValue, _ := strconv.Atoi(strings.Split(leftParts[index], "-")[0])
		rightValue, _ := strconv.Atoi(strings.Split(rightParts[index], "-")[0])
		if leftValue != rightValue {
			return leftValue > rightValue
		}
	}
	return left > right
}

// LocalPackageReadme reads only the README belonging to a discovered backpack
// entry. It never accepts a caller-provided path, so the endpoint cannot be
// used to browse arbitrary project files.
func (m Manager) LocalPackageReadme(root, packageName string) (Result, error) {
	items, err := m.LocalPackages(root)
	if err != nil {
		return Result{}, err
	}
	for _, item := range items {
		if item.Name != packageName {
			continue
		}
		path := filepath.Join(item.Path, "README.md")
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return Result{}, errors.New("这个本地包没有 README.md")
		}
		if info.Size() > maxMCPFileSize {
			return Result{}, errors.New("README.md 过大，无法显示")
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return Result{}, errors.New("无法读取 README.md")
		}
		return Result{Path: path, Output: string(data)}, nil
	}
	return Result{}, errors.New("背包中没有这个本地包")
}

// RuntimeOverview is the small, stable set of project facts needed by the
// dashboard's run page. Packages are checked on disk, not only in
// package.json: declaring a platform is not the same as having installed it.
type RuntimeOverview struct {
	Name           string           `json:"name"`
	Version        string           `json:"version"`
	PackageManager string           `json:"packageManager"`
	HasAppScript   bool             `json:"hasAppScript"`
	HasDevScript   bool             `json:"hasDevScript"`
	HasBuildScript bool             `json:"hasBuildScript"`
	HasStartScript bool             `json:"hasStartScript"`
	PM2Configured  bool             `json:"pm2Configured"`
	Platforms      []RuntimePackage `json:"platforms"`
}

type RuntimePackage struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Package   string `json:"package"`
	Declared  bool   `json:"declared"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
}

type RuntimePreflight struct {
	Login                string   `json:"login"`
	Package              string   `json:"package,omitempty"`
	Missing              []string `json:"missing"`
	Summary              []string `json:"summary"`
	DependenciesComplete bool     `json:"dependenciesComplete"`
}

var runtimePlatforms = []struct{ id, label, pkg string }{
	{"onebot", "OneBot", "@alemonjs/onebot"},
	{"qq-bot", "QQ Bot", "@alemonjs/qq-bot"},
	{"discord", "Discord", "@alemonjs/discord"},
	{"bubble", "Bubble", "@alemonjs/bubble"},
	{"kook", "KOOK", "@alemonjs/kook"},
	{"telegram", "Telegram", "@alemonjs/telegram"},
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
		if permissionError(err) {
			return Result{}, permissionAdvice("访问 " + filepath.Dir(name))
		}
		return Result{}, errors.New("目标目录不存在；MCP 不会自动创建目录")
	}
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return Result{}, errors.New("只能写入普通项目文件")
	} else if err != nil && !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("无法检查目标文件：%w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		if permissionError(err) {
			return Result{}, permissionAdvice("保存 " + filepath.Base(path))
		}
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

func (Manager) RuntimeOverview(root string) (RuntimeOverview, error) {
	path, err := projectPath(root)
	if err != nil {
		return RuntimeOverview{}, err
	}
	var manifest struct {
		Name            string            `json:"name"`
		Version         string            `json:"version"`
		PackageManager  string            `json:"packageManager"`
		Scripts         map[string]string `json:"scripts"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil || json.Unmarshal(data, &manifest) != nil {
		return RuntimeOverview{}, errors.New("无法读取 package.json")
	}
	overview := RuntimeOverview{Name: manifest.Name, Version: manifest.Version, PackageManager: projectPackageManager(path), HasAppScript: manifest.Scripts["app"] != "", HasDevScript: manifest.Scripts["dev"] != "", HasBuildScript: manifest.Scripts["build"] != "", HasStartScript: manifest.Scripts["start"] != ""}
	if manifest.PackageManager != "" {
		overview.PackageManager = strings.Split(manifest.PackageManager, "@")[0]
	}
	if _, err := os.Stat(filepath.Join(path, "pm2.config.cjs")); err == nil {
		overview.PM2Configured = true
	}
	for _, platform := range runtimePlatforms {
		item := RuntimePackage{ID: platform.id, Label: platform.label, Package: platform.pkg}
		_, dependency := manifest.Dependencies[item.Package]
		_, devDependency := manifest.DevDependencies[item.Package]
		item.Declared = dependency || devDependency
		packageFile := filepath.Join(path, "node_modules", filepath.FromSlash(item.Package), "package.json")
		if packageData, readErr := os.ReadFile(packageFile); readErr == nil {
			var installed struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(packageData, &installed) == nil {
				item.Installed = true
				item.Version = installed.Version
			}
		}
		overview.Platforms = append(overview.Platforms, item)
	}
	return overview, nil
}

// RuntimePreflight describes the effective, non-secret connection settings
// before a command is allowed to start. Values are deliberately reduced to
// “configured / missing” so tokens never enter the browser response.
func (m Manager) RuntimePreflight(root string) (RuntimePreflight, error) {
	path, err := projectPath(root)
	if err != nil {
		return RuntimePreflight{}, err
	}
	content, err := os.ReadFile(filepath.Join(path, "alemon.config.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return RuntimePreflight{}, fmt.Errorf("无法读取机器人运行配置：%w", err)
	}
	preflight := RuntimePreflight{Missing: []string{}, Summary: []string{}}
	dependencies, dependencyErr := m.RuntimeDependencies(root)
	if dependencyErr != nil {
		return RuntimePreflight{}, dependencyErr
	}
	preflight.DependenciesComplete = len(dependencies) == 0
	if preflight.DependenciesComplete {
		preflight.Summary = append(preflight.Summary, "项目依赖：完整")
	} else {
		preflight.Missing = append(preflight.Missing, dependencies...)
		preflight.Summary = append(preflight.Summary, "项目依赖：需要重载")
	}
	match := regexp.MustCompile(`(?m)^login:\s*['\"]?([^'\"\r\n#]+)`).FindStringSubmatch(string(content))
	if len(match) < 2 || strings.TrimSpace(match[1]) == "" {
		preflight.Summary = append(preflight.Summary, "登录连接：未配置（可选择无 login 启动）")
		return preflight, nil
	}
	preflight.Login = strings.TrimSpace(match[1])
	preflight.Summary = append(preflight.Summary, "登录连接："+preflight.Login)
	for _, platform := range runtimePlatforms {
		if platform.id != preflight.Login {
			continue
		}
		preflight.Package = platform.pkg
		// PackageConfig intentionally returns an error for a valid connection
		// package that simply has no alemonjs.config declaration. That must not
		// be reported as “not installed”: installation and optional configuration
		// are separate facts.
		if _, _, installedErr := installedPackageManifest(path, platform.pkg); installedErr != nil {
			preflight.Missing = append(preflight.Missing, "连接包 "+platform.pkg+" 未安装")
			return preflight, nil
		}
		definition, configErr := m.PackageConfig(root, platform.pkg)
		if configErr != nil {
			if strings.Contains(configErr.Error(), "没有声明 alemonjs.config") {
				preflight.Summary = append(preflight.Summary, "连接包 "+platform.pkg+"：已安装，无额外配置")
				return preflight, nil
			}
			preflight.Missing = append(preflight.Missing, "连接包 "+platform.pkg+" 配置无法读取")
			return preflight, nil
		}
		for _, field := range definition.Fields {
			label := field.Description
			if label == "" {
				label = field.Name
			}
			configured := strings.TrimSpace(definition.Values[field.Name]) != ""
			if field.Required && !configured {
				preflight.Missing = append(preflight.Missing, label)
			}
			preflight.Summary = append(preflight.Summary, label+map[bool]string{true: "：已填写", false: "：未填写"}[configured])
		}
		return preflight, nil
	}
	preflight.Summary = append(preflight.Summary, "自定义登录对象：未声明可校验字段")
	return preflight, nil
}

// RuntimeDependencies checks every direct project dependency on disk. A lock
// file alone is not enough: a partially completed install can leave the bot
// unable to start even though package.json looks correct.
func (Manager) RuntimeDependencies(root string) ([]string, error) {
	path, err := projectPath(root)
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil || json.Unmarshal(data, &manifest) != nil {
		return nil, errors.New("无法读取 package.json")
	}
	if _, err := os.Stat(filepath.Join(path, "node_modules")); err != nil {
		if os.IsNotExist(err) {
			return []string{"未发现 node_modules，请先重载依赖"}, nil
		}
		return nil, fmt.Errorf("无法检查 node_modules：%w", err)
	}
	packages := map[string]bool{}
	for name := range manifest.Dependencies {
		packages[name] = true
	}
	for name := range manifest.DevDependencies {
		packages[name] = true
	}
	missing := []string{}
	for name := range packages {
		if _, err := os.Stat(filepath.Join(path, "node_modules", filepath.FromSlash(name), "package.json")); err != nil {
			if os.IsNotExist(err) {
				missing = append(missing, name+" 未安装")
				continue
			}
			return nil, fmt.Errorf("无法检查依赖 %s：%w", name, err)
		}
	}
	sort.Strings(missing)
	return missing, nil
}

type alemonUpgradePlan struct {
	Dependencies    []string
	DevDependencies []string
	Workspace       bool
}

// UpgradeAlemonDependencies upgrades only the framework and its official
// scoped packages declared directly by this robot. Business dependencies stay
// untouched, so the one-click action is safe to use as framework maintenance.
func (m Manager) UpgradeAlemonDependencies(root string) (Result, error) {
	path, err := projectPath(root)
	if err != nil {
		return Result{}, err
	}
	plan, err := readAlemonUpgradePlan(path)
	if err != nil {
		return Result{}, err
	}
	packages := append(append([]string{}, plan.Dependencies...), plan.DevDependencies...)
	if len(packages) == 0 {
		return Result{Path: path, Output: "package.json 中未声明 alemonjs 或 @alemonjs/ 依赖，无需升级。"}, nil
	}
	manager := projectPackageManager(path)
	latest := func(items []string) []string {
		result := make([]string, 0, len(items))
		for _, item := range items {
			result = append(result, item+"@latest")
		}
		return result
	}
	var outputs []string
	runUpgrade := func(args ...string) error {
		output, runErr := runNamedPackageManager(path, manager, args...)
		if output != "" {
			outputs = append(outputs, output)
		}
		return runErr
	}
	switch manager {
	case "npm":
		if len(plan.Dependencies) > 0 {
			if err := runUpgrade(append([]string{"install", "--save"}, latest(plan.Dependencies)...)...); err != nil {
				return Result{Path: path, Output: strings.Join(outputs, "\n")}, err
			}
		}
		if len(plan.DevDependencies) > 0 {
			if err := runUpgrade(append([]string{"install", "--save-dev"}, latest(plan.DevDependencies)...)...); err != nil {
				return Result{Path: path, Output: strings.Join(outputs, "\n")}, err
			}
		}
	default:
		args := []string{"upgrade", "--latest"}
		if manager == "pnpm" {
			args[0] = "update"
		}
		if plan.Workspace {
			if manager == "pnpm" {
				args = append(args, "-w")
			}
		}
		if err := runUpgrade(append(args, packages...)...); err != nil {
			return Result{Path: path, Output: strings.Join(outputs, "\n")}, err
		}
	}
	return Result{Path: path, Output: "已升级 AlemonJS 依赖：" + strings.Join(packages, "、") + ".\n" + strings.Join(outputs, "\n")}, nil
}

func readAlemonUpgradePlan(root string) (alemonUpgradePlan, error) {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return alemonUpgradePlan{}, errors.New("无法读取 package.json")
	}
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
		Workspaces      json.RawMessage   `json:"workspaces"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return alemonUpgradePlan{}, errors.New("package.json 格式无法识别")
	}
	filter := func(values map[string]string) []string {
		result := []string{}
		for name := range values {
			if name == "alemonjs" || strings.HasPrefix(name, "@alemonjs/") {
				result = append(result, name)
			}
		}
		sort.Strings(result)
		return result
	}
	return alemonUpgradePlan{
		Dependencies:    filter(manifest.Dependencies),
		DevDependencies: filter(manifest.DevDependencies),
		Workspace:       len(manifest.Workspaces) > 0 && string(manifest.Workspaces) != "null",
	}, nil
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

func (Manager) PM2Logs(root string, page int) (PM2LogPage, error) {
	if _, err := projectPath(root); err != nil {
		return PM2LogPage{}, err
	}
	if page < 1 {
		page = 1
	}
	const pageSize = 120
	requested := page * pageSize
	output, err := run(root, "npx", "pm2", "logs", "--lines", strconv.Itoa(requested), "--nostream")
	if err != nil {
		return PM2LogPage{}, fmt.Errorf("无法读取 PM2 日志：%w", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	return paginatePM2LogLines(lines, page), nil
}

func (Manager) PM2Status(root string) (PM2Status, error) {
	path, err := projectPath(root)
	if err != nil {
		return PM2Status{}, err
	}
	if _, err := os.Stat(filepath.Join(path, "pm2.config.cjs")); err != nil {
		if os.IsNotExist(err) {
			return PM2Status{}, nil
		}
		return PM2Status{}, fmt.Errorf("无法读取 PM2 配置：%w", err)
	}
	output, err := run(path, "npx", "pm2", "jlist")
	if err != nil {
		return PM2Status{}, fmt.Errorf("无法读取 PM2 状态：%w", err)
	}
	return parsePM2Status(path, output)
}

func parsePM2Status(root, output string) (PM2Status, error) {
	var processes []struct {
		PM2Env struct {
			CWD    string `json:"pm_cwd"`
			Status string `json:"status"`
		} `json:"pm2_env"`
	}
	if err := json.Unmarshal([]byte(output), &processes); err != nil {
		return PM2Status{}, fmt.Errorf("无法解析 PM2 状态：%w", err)
	}
	root = filepath.Clean(root)
	for _, process := range processes {
		if filepath.Clean(process.PM2Env.CWD) != root {
			continue
		}
		status := strings.ToLower(process.PM2Env.Status)
		return PM2Status{
			Configured: true,
			Managed:    true,
			Running:    status == "online" || status == "launching",
			Status:     status,
		}, nil
	}
	return PM2Status{Configured: true, Status: "not-found"}, nil
}

func paginatePM2LogLines(lines []string, page int) PM2LogPage {
	if page < 1 {
		page = 1
	}
	const pageSize = 120
	end := len(lines) - (page-1)*pageSize
	if end <= 0 {
		return PM2LogPage{Output: "没有更早的 PM2 日志。", Page: page}
	}
	start := end - pageSize
	if start < 0 {
		start = 0
	}
	result := strings.Join(lines[start:end], "\n")
	if result == "" {
		result = "PM2 暂无可读取的日志。"
	}
	return PM2LogPage{Output: result, Page: page, HasOlder: start > 0}
}

// DevelopmentCommand prepares the project's declared development command for
// the web server to supervise. Its stdout and stderr stay attached to the
// operation record, so the UI can show progress without exposing a shell.
func (Manager) DevelopmentCommand(root string) (*exec.Cmd, error) {
	return (Manager{}).scriptCommand(root, "dev")
}

// ForegroundCommand runs the project's declared `app` script under the same
// supervised terminal used for development mode.
func (Manager) ForegroundCommand(root string) (*exec.Cmd, error) {
	return (Manager{}).scriptCommand(root, "app")
}

func (Manager) scriptCommand(root, script string) (*exec.Cmd, error) {
	if err := project(root); err != nil {
		return nil, err
	}
	if script == "dev" {
		if err := fixLegacyLvyScript(root); err != nil {
			return nil, err
		}
	}
	command, _ := PackageManagerCommand(root, "run", script)
	if filepath.Base(command.Path) == "npx" {
		if _, err := exec.LookPath("npx"); err != nil {
			return nil, missingCommandAdvice("npx")
		}
	}
	return command, nil
}

func (Manager) RepairRuntime(root, mode string) (Result, error) {
	path, err := projectPath(root)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return Result{}, err
	}
	var manifest map[string]any
	if json.Unmarshal(data, &manifest) != nil {
		return Result{}, errors.New("无法读取 package.json")
	}
	scripts, _ := manifest["scripts"].(map[string]any)
	if scripts == nil {
		scripts = map[string]any{}
		manifest["scripts"] = scripts
	}
	dependencies, _ := manifest["devDependencies"].(map[string]any)
	if dependencies == nil {
		dependencies = map[string]any{}
		manifest["devDependencies"] = dependencies
	}
	if mode == "dev" {
		if _, ok := scripts["app"]; !ok {
			scripts["app"] = "node index.js"
		}
		if _, ok := scripts["dev"]; !ok {
			scripts["dev"] = scripts["app"]
		}
	}
	if mode == "pm2" {
		scripts["start"] = "npx pm2 startOrRestart pm2.config.cjs"
		scripts["stop"] = "npx pm2 stop pm2.config.cjs"
		dependencies["pm2"] = "^5"
		dependencies["yaml"] = "^2.6.0"
		config := filepath.Join(path, "pm2.config.cjs")
		pm2Config := "const pm2 = globalThis.pm2;\n\nmodule.exports = pm2 || {\n  apps: [\n    {\n      name: 'alemonb',\n      script: 'node index.js',\n      env: {\n        NODE_ENV: 'production'\n      }\n    }\n  ]\n};\n"
		if err := os.WriteFile(config, []byte(pm2Config), 0644); err != nil {
			if permissionError(err) {
				return Result{}, permissionAdvice("保存 PM2 配置")
			}
			return Result{}, fmt.Errorf("无法写入 PM2 配置：%w", err)
		}
		entry := filepath.Join(path, "index.js")
		if _, statErr := os.Stat(entry); os.IsNotExist(statErr) {
			if err := os.WriteFile(entry, []byte("import { start } from 'alemonjs';\n\nstart();\n"), 0644); err != nil {
				if permissionError(err) {
					return Result{}, permissionAdvice("保存生产入口")
				}
				return Result{}, fmt.Errorf("无法写入生产入口：%w", err)
			}
		}
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(path, "package.json"), append(encoded, '\n'), 0644); err != nil {
		if permissionError(err) {
			return Result{}, permissionAdvice("保存 package.json")
		}
		return Result{}, err
	}
	return Result{Path: path, Output: "已补齐运行脚本与配置，请安装依赖后重试。"}, nil
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
			return Result{}, permissionAdvice("读取机器人目录")
		}
		return Result{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// These two files are editable project configuration. A new robot
		// project legitimately may not have created either one yet; present an
		// empty document so the editor can create it on save.
		if errors.Is(err, os.ErrNotExist) && (name == "alemon.config.yaml" || name == ".npmrc" || name == ".env") {
			return Result{Path: path, Output: ""}, nil
		}
		if permissionError(err) {
			return Result{}, permissionAdvice("读取 " + filepath.Base(path))
		}
		return Result{}, fmt.Errorf("读取失败：%w", err)
	}
	return Result{Path: path, Output: string(data)}, nil
}
func (m Manager) Write(root, name, content string) (Result, error) {
	path, err := file(root, name)
	if err != nil {
		if permissionError(err) {
			return Result{}, permissionAdvice("访问机器人目录")
		}
		return Result{}, err
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		if !permissionError(err) {
			return Result{}, fmt.Errorf("保存 %s 失败：%w", filepath.Base(path), err)
		}
		return Result{}, permissionAdvice("保存 " + filepath.Base(path))
	}
	return Result{Path: path, Output: "已保存。"}, nil
}
func (m Manager) Run(root, action, message, packageName, version, tag, token string, confirmed bool) (Result, error) {
	if action == "git-release" {
		if _, err := workspacePath(root); permissionError(err) {
			return Result{}, permissionAdvice("访问 Git 工作区")
		}
		// message carries the source commit selected in the Git publishing card.
		// Keeping it on the existing task payload also preserves privileged runs.
		var request struct {
			Branch    string   `json:"branch"`
			Commit    string   `json:"commit"`
			Artifacts []string `json:"artifacts"`
		}
		if json.Unmarshal([]byte(message), &request) == nil && request.Commit != "" {
			if len(request.Artifacts) == 0 {
				return Result{}, errors.New("请至少选择一个最终发布产物")
			}
			return GitPublishWithOptions(root, version, request.Branch, request.Commit, request.Artifacts, confirmed)
		}
		return GitPublish(root, version, message, confirmed)
	}
	if err := project(root); err != nil {
		if permissionError(err) {
			return Result{}, permissionAdvice("访问机器人目录")
		}
		return Result{}, err
	}
	if action == "dev" || action == "app" || action == "pm2" {
		missing, err := (Manager{}).RuntimeDependencies(root)
		if err != nil {
			return Result{}, err
		}
		if len(missing) > 0 {
			return Result{}, errors.New("项目依赖不完整：" + strings.Join(missing, "、") + "。请先执行“重载依赖”后再运行")
		}
	}
	manager := projectPackageManager(root)
	var name string
	var args []string
	switch action {
	case "dependency-status":
		missing, err := (Manager{}).RuntimeDependencies(root)
		if err != nil {
			return Result{}, err
		}
		checks := []string{"依赖按 package.json 中声明的 " + strings.ToUpper(manager) + " 管理。"}
		if len(missing) == 0 {
			checks = append(checks, "已检查全部直接依赖，当前安装完整。")
		} else {
			checks = append(checks, "依赖不完整："+strings.Join(missing, "、"), "请执行“重新安装依赖”后再运行。")
		}
		return Result{Path: root, Output: strings.Join(checks, "\n")}, nil
	case "install":
		name, args = manager, []string{"install"}
	case "upgrade-alemon":
		return (Manager{}).UpgradeAlemonDependencies(root)
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
		name, args = manager, packageVersionArgs(manager, version)
	case "dev":
		if err := fixLegacyLvyScript(root); err != nil {
			return Result{}, err
		}
		name, args = manager, []string{"run", "dev"}
	case "app":
		name, args = manager, []string{"run", "app"}
	case "repair-dev":
		return (Manager{}).RepairRuntime(root, "dev")
	case "repair-pm2":
		return (Manager{}).RepairRuntime(root, "pm2")
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
		name, args = manager, []string{"run", "start"}
	case "pm2-stop":
		name, args = manager, []string{"run", "stop"}
	case "pm2-restart":
		name, args = "npx", []string{"pm2", "restart", "pm2.config.cjs", "--update-env"}
	case "pm2-reload":
		name, args = "npx", []string{"pm2", "reload", "pm2.config.cjs", "--update-env"}
	case "pm2-delete":
		name, args = "npx", []string{"pm2", "delete", "pm2.config.cjs"}
	case "pm2-status":
		name, args = "npx", []string{"pm2", "list"}
	case "pm2-logs":
		name, args = "npx", []string{"pm2", "logs", "--lines", "120", "--nostream"}
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
	case "remove-local-package":
		return removeLocalPackageByName(root, packageName)
	case "replace-local-package":
		return replaceLocalPackage(root, packageName, version)
	case "switch-local-package-version":
		return switchLocalPackageVersion(root, packageName, version)
	case "install-connection":
		if !allowedInstallPackage(packageName) {
			return Result{}, errors.New("连接包名无效")
		}
		return installConnectionPackage(root, packageName)
	case "uninstall-connection":
		if !allowedPackage(packageName) {
			return Result{}, errors.New("连接包名无效")
		}
		return removeConnectionPackage(root, packageName)
	case "git-init":
		if _, err := run(root, "git", "init"); err != nil {
			return Result{}, err
		}
		name, args = "git", []string{"branch", "-M", "main"}
	default:
		return Result{}, errors.New("未知的机器人操作")
	}
	var output string
	var runErr error
	if name == manager {
		output, runErr = runPackageManager(root, args...)
	} else {
		output, runErr = run(root, name, args...)
	}
	return Result{Path: root, Output: output}, runErr
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
	requestFile, err := os.CreateTemp("", "alx-robot-request-*.json")
	if err != nil {
		return Result{}, fmt.Errorf("无法准备权限申请：%w", err)
	}
	requestPath := requestFile.Name()
	defer os.Remove(requestPath)
	resultFile, err := os.CreateTemp("", "alx-robot-result-*.json")
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
	// Packages are executed through the selected package manager without a
	// shell. Accept a normal npm name so a user can register a custom platform,
	// but reject flags, paths and arbitrary command text.
	return regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`).MatchString(name)
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

// permissionAdvice deliberately stays inside the web operation flow.  A
// background install or save must never surprise a beginner with an operating
// system administrator dialog.  The dashboard preserves this exact message in
// its operation record and toast, alongside the directory picker guidance.
func permissionAdvice(action string) error {
	return fmt.Errorf("没有权限%s。请在系统设置中为 alx 授予该磁盘或文件夹的访问权限（macOS：\"文件与文件夹\"或\"完全磁盘访问\"），或选择当前登录账户可读写的目录后重试", action)
}
func file(root, name string) (string, error) {
	if err := project(root); err != nil {
		return "", err
	}
	if name != ".npmrc" && name != ".env" && name != "alemon.config.yaml" && name != "README.md" {
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
		if permissionError(err) {
			return permissionAdvice("更新开发脚本")
		}
		return fmt.Errorf("无法更新开发脚本：%w", err)
	}
	return nil
}
func run(root, name string, args ...string) (string, error) {
	return runWithEnv(root, nil, name, args...)
}

// PackageManagerCommand keeps a robot usable when package.json requests a
// package manager that is not globally installed. npx uses the npm bundled
// with Node.js and needs no administrator write permission.
func PackageManagerCommand(root string, args ...string) (*exec.Cmd, string) {
	return packageManagerCommand(root, projectPackageManager(root), args...)
}

func packageManagerCommand(root, manager string, args ...string) (*exec.Cmd, string) {
	if _, err := exec.LookPath(manager); err == nil {
		command := exec.Command(manager, args...)
		command.Dir = root
		return command, ""
	}
	packageName := ""
	switch manager {
	case "yarn":
		packageName = "yarn@1.22.22"
	case "pnpm":
		packageName = "pnpm@latest"
	default:
		command := exec.Command(manager, args...)
		command.Dir = root
		return command, ""
	}
	command := exec.Command("npx", append([]string{"--yes", packageName}, args...)...)
	command.Dir = root
	return command, "未找到 " + strings.ToUpper(manager) + "，临时通过 npm 获取并执行；不会修改电脑的全局安装。"
}

func runPackageManager(root string, args ...string) (string, error) {
	return runNamedPackageManager(root, projectPackageManager(root), args...)
}

func packageVersionArgs(manager, version string) []string {
	if manager == "yarn" {
		return []string{"version", "--new-version", version, "--no-git-tag-version"}
	}
	return []string{"version", version, "--no-git-tag-version"}
}

func runNamedPackageManager(root, manager string, args ...string) (string, error) {
	command, notice := packageManagerCommand(root, manager, args...)
	values := packageManagerEnvironment(root)
	if filepath.Base(command.Path) == "npx" {
		// npm reads project .npmrc before it launches temporary Yarn/PNPM.
		// Settings owned by another package manager (for example node-linker)
		// are harmless, and must not bury the actual operation result.
		values["NPM_CONFIG_LOGLEVEL"] = "error"
	}
	output, err := runWithEnv(root, values, command.Path, command.Args[1:]...)
	if notice != "" {
		if output != "" {
			output = notice + "\n" + output
		} else {
			output = notice
		}
	}
	return output, err
}

// packageManagerEnvironment makes GUI-launched setup processes honour the
// user's Node version manager. On macOS a process launched before nvm is
// loaded often keeps Homebrew's non-LTS Node in PATH, while the terminal has
// the intended LTS version. Prefer .nvmrc, then nvm's default alias.
func packageManagerEnvironment(root string) map[string]string {
	values := map[string]string{}
	if bin := preferredNVMNodeBin(root); bin != "" {
		values["PATH"] = bin + string(os.PathListSeparator) + os.Getenv("PATH")
	}
	return values
}

func preferredNVMNodeBin(root string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	wanted := ""
	if data, err := os.ReadFile(filepath.Join(root, ".nvmrc")); err == nil {
		wanted = strings.TrimPrefix(strings.TrimSpace(string(data)), "v")
	}
	if wanted == "" {
		if data, err := os.ReadFile(filepath.Join(home, ".nvm", "alias", "default")); err == nil {
			wanted = strings.TrimPrefix(strings.TrimSpace(string(data)), "v")
		}
	}
	if wanted == "" || strings.ContainsAny(wanted, "/ ") {
		return ""
	}
	versions, err := os.ReadDir(filepath.Join(home, ".nvm", "versions", "node"))
	if err != nil {
		return ""
	}
	candidates := []string{}
	for _, entry := range versions {
		name := strings.TrimPrefix(entry.Name(), "v")
		if entry.IsDir() && nodeVersionMatches(name, wanted) {
			if info, err := os.Stat(filepath.Join(home, ".nvm", "versions", "node", entry.Name(), "bin", "node")); err == nil && !info.IsDir() {
				candidates = append(candidates, entry.Name())
			}
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Slice(candidates, func(i, j int) bool { return nodeVersionGreater(candidates[i], candidates[j]) })
	return filepath.Join(home, ".nvm", "versions", "node", candidates[0], "bin")
}

func nodeVersionMatches(version, wanted string) bool {
	version, wanted = strings.TrimPrefix(version, "v"), strings.TrimPrefix(wanted, "v")
	if version == wanted {
		return true
	}
	return regexp.MustCompile(`^\d+$`).MatchString(wanted) && strings.HasPrefix(version, wanted+".")
}

func nodeVersionGreater(left, right string) bool {
	var l1, l2, l3, r1, r2, r3 int
	_, _ = fmt.Sscanf(strings.TrimPrefix(left, "v"), "%d.%d.%d", &l1, &l2, &l3)
	_, _ = fmt.Sscanf(strings.TrimPrefix(right, "v"), "%d.%d.%d", &r1, &r2, &r3)
	for _, pair := range [][2]int{{l1, r1}, {l2, r2}, {l3, r3}} {
		if pair[0] != pair[1] {
			return pair[0] > pair[1]
		}
	}
	return false
}

func runWithEnv(root string, values map[string]string, name string, args ...string) (string, error) {
	timeout := commandTimeout(name, args...)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = root
	cmd.Env = os.Environ()
	for key, value := range values {
		prefix := key + "="
		for index := len(cmd.Env) - 1; index >= 0; index-- {
			if strings.HasPrefix(cmd.Env[index], prefix) {
				cmd.Env[index] = prefix + value
				prefix = ""
				break
			}
		}
		if prefix != "" {
			cmd.Env = append(cmd.Env, prefix+value)
		}
	}
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return text, fmt.Errorf("操作超时（%s）；请检查网络、登录状态或代理后重试", timeout.Round(time.Second))
	}
	if err != nil {
		if permissionError(err) || permissionError(errors.New(text)) {
			return text, permissionAdvice("执行 " + filepath.Base(name))
		}
		if commandNotFound(err, text) {
			return text, missingCommandAdvice(filepath.Base(name))
		}
		if text != "" {
			return text, fmt.Errorf("%s：%w", text, err)
		}
		return text, fmt.Errorf("执行 %s 失败：%w", strings.Join(append([]string{filepath.Base(name)}, args...), " "), err)
	}
	return text, nil
}

func commandTimeout(name string, args ...string) time.Duration {
	base := strings.ToLower(filepath.Base(name))
	if base == "git" && len(args) > 0 {
		switch args[0] {
		case "clone", "fetch", "pull", "push", "ls-remote":
			return 10 * time.Minute
		}
	}
	if base == "npm" || base == "yarn" || base == "pnpm" || base == "npx" {
		return 20 * time.Minute
	}
	return 2 * time.Minute
}

func commandNotFound(err error, output string) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	text := strings.ToLower(err.Error() + "\n" + output)
	return strings.Contains(text, "executable file not found") || strings.Contains(text, "command not found") || strings.Contains(text, "is not recognized as an internal or external command")
}

func missingCommandAdvice(name string) error {
	switch strings.ToLower(name) {
	case "node", "npm", "npx", "yarn", "pnpm":
		return errors.New("未检测到 Node.js 运行环境（含 npm/npx）。请先在左上角“环境”中安装 Node.js LTS，完成后重新执行；Yarn 和 PNPM 无需全局安装，alx 会临时执行它们")
	case "git":
		return errors.New("未检测到 Git。请先在左上角“环境”中安装 Git，完成后重新执行")
	default:
		return fmt.Errorf("未检测到 %s 命令。请安装对应的系统工具后重新执行", name)
	}
}
