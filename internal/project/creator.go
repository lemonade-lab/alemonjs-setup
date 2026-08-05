// Package project creates a configured AlemonJS project from the bundled templates.
package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"alemonjs-setup/internal/system"
)

type Config struct {
	Template        string `json:"template"`
	Name            string `json:"name"`
	DestinationMode string `json:"destinationMode"`
	Destination     string `json:"destination"`
	Language        string `json:"language"`
	PackageManager  string `json:"packageManager"`
	ESLint          bool   `json:"eslint"`
	InitializeGit   bool   `json:"initializeGit"`
	UsePM2          bool   `json:"usePM2"`
	ImageMode       string `json:"imageMode"`
	StyleMode       string `json:"styleMode"`
	DownloadSkills  bool   `json:"downloadSkills"`
}

type Result struct {
	Path   string   `json:"path"`
	Status string   `json:"status"`
	Logs   []string `json:"logs"`
}

type Creator struct{ templates fs.FS }

func NewCreator(templates fs.FS) *Creator { return &Creator{templates: templates} }

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func (c *Creator) Create(config Config) (Result, error) {
	if err := validate(config); err != nil {
		return Result{}, err
	}
	if config.DestinationMode == "current" {
		current, err := os.Getwd()
		if err != nil {
			return Result{}, errors.New("无法读取当前运行目录")
		}
		config.Destination = current
	}
	path := filepath.Join(config.Destination, config.Name)
	if _, err := os.Stat(path); err == nil {
		return Result{}, errors.New("目标文件夹已经存在；请换一个项目名称或保存位置，工具不会覆盖已有文件")
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("无法检查目标文件夹：%w", err)
	}
	if info, err := os.Stat(config.Destination); err != nil {
		if isPermissionError(err) {
			return c.createWithPrivileges(config)
		}
		return Result{}, errors.New("保存位置不存在或不是文件夹，请重新选择")
	} else if !info.IsDir() {
		return Result{}, errors.New("保存位置不存在或不是文件夹，请重新选择")
	}
	if err := ensureWritableDirectory(config.Destination); err != nil {
		if isPermissionError(err) {
			return c.createWithPrivileges(config)
		}
		return Result{}, err
	}

	result := Result{Path: path, Status: "failed"}
	log := func(message string) { result.Logs = append(result.Logs, message) }
	template := config.Template
	if template == "" {
		template = "dev"
	}
	log("正在创建项目文件夹…")
	if err := copyTemplate(c.templates, template, path); err != nil {
		return result, fmt.Errorf("复制内置模板失败：%w", err)
	}
	log(fmt.Sprintf("已复制 %s 模板。", map[string]string{"bot": "机器人", "dev": "开发"}[template]))
	if err := patchPackage(path, config); err != nil {
		return result, fmt.Errorf("写入项目配置失败：%w", err)
	}
	log("已按你的选择配置项目。")

	packageCommand := config.PackageManager
	if config.PackageManager == "yarn" {
		if _, err := exec.LookPath("yarn"); err != nil {
			// Global npm installation commonly fails for non-admin users. Use an
			// ephemeral npx invocation instead, leaving the user's system intact.
			log("未找到 Yarn，临时使用 npm 下载 Yarn；不会修改电脑的全局安装。")
			packageCommand = "npx"
		}
	}
	log("正在安装项目依赖…")
	install := map[string][]string{"yarn": {"install"}, "npm": {"install"}, "pnpm": {"install"}}[config.PackageManager]
	if packageCommand == "npx" {
		install = append([]string{"--yes", "yarn@1.22.22"}, install...)
	}
	if err := run(path, &result.Logs, packageCommand, install...); err != nil {
		if packageCommand == "npx" {
			return result, fmt.Errorf("临时使用 Yarn 安装依赖失败；请返回“包管理器”步骤选择 npm 后重试：%w", err)
		}
		return result, fmt.Errorf("安装项目依赖失败：%w", err)
	}

	if config.InitializeGit {
		log("正在初始化 Git 存档…")
		for _, command := range [][]string{{"init"}, {"config", "user.name", "AlemonJS Setup"}, {"config", "user.email", "setup@alemonjs.local"}, {"add", "."}, {"commit", "-m", "chore: initialize alemonjs project"}} {
			if err := run(path, &result.Logs, "git", command...); err != nil {
				return result, fmt.Errorf("初始化 Git 失败：%w", err)
			}
		}
	}
	if config.DownloadSkills {
		log("正在下载 AlemonJS 开发技能…")
		if err := run(path, &result.Logs, "git", "clone", "--depth", "1", "https://github.com/lemonade-lab/alemonjs-dev-skill.git", ".skills/alemonjs-dev-skill"); err != nil {
			return result, fmt.Errorf("下载开发技能失败：%w", err)
		}
	}
	result.Status = "ready"
	log("项目创建完成。")
	return result, nil
}

type elevatedResult struct {
	Result Result `json:"result"`
	Error  string `json:"error,omitempty"`
}

// createWithPrivileges hands the complete creation flow to the same executable
// after the OS has authenticated it.  This is necessary because copying a
// template involves many filesystem writes, not just a single shell command.
func (c *Creator) createWithPrivileges(config Config) (Result, error) {
	executable, err := os.Executable()
	if err != nil {
		return Result{}, fmt.Errorf("无法申请系统权限：%w", err)
	}
	configFile, err := os.CreateTemp("", "albs-create-request-*.json")
	if err != nil {
		return Result{}, fmt.Errorf("无法准备权限申请：%w", err)
	}
	configPath := configFile.Name()
	defer os.Remove(configPath)
	resultFile, err := os.CreateTemp("", "albs-create-result-*.json")
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
	data, err := json.Marshal(config)
	if err != nil {
		return Result{}, fmt.Errorf("无法准备权限申请：%w", err)
	}
	if _, err := configFile.Write(data); err != nil {
		return Result{}, fmt.Errorf("无法准备权限申请：%w", err)
	}
	if err := configFile.Close(); err != nil {
		return Result{}, fmt.Errorf("无法准备权限申请：%w", err)
	}
	if _, err := system.RunWithPrivileges("", nil, executable, "--privileged-create", configPath, resultPath); err != nil {
		return Result{}, err
	}
	data, err = os.ReadFile(resultPath)
	if err != nil {
		return Result{}, fmt.Errorf("权限操作未返回结果：%w", err)
	}
	var response elevatedResult
	if err := json.Unmarshal(data, &response); err != nil {
		return Result{}, fmt.Errorf("权限操作返回格式无法识别：%w", err)
	}
	if response.Error != "" {
		return response.Result, errors.New(response.Error)
	}
	return response.Result, nil
}

func validate(c Config) error {
	if c.Template != "" && c.Template != "bot" && c.Template != "dev" {
		return errors.New("项目模板无效")
	}
	if !validName.MatchString(c.Name) {
		return errors.New("项目名称只能使用字母、数字、点、下划线或短横线，且必须以字母或数字开头")
	}
	if c.DestinationMode != "current" && c.DestinationMode != "custom" {
		return errors.New("创建位置无效")
	}
	if c.DestinationMode == "custom" && !filepath.IsAbs(c.Destination) {
		return errors.New("保存位置必须是本机的完整文件夹路径")
	}
	if c.Language != "js" && c.Language != "ts" {
		return errors.New("开发语言无效")
	}
	if c.PackageManager != "yarn" && c.PackageManager != "npm" && c.PackageManager != "pnpm" {
		return errors.New("包管理器无效")
	}
	if c.ImageMode != "none" && c.ImageMode != "html" && c.ImageMode != "react" {
		return errors.New("图片开发方式无效")
	}
	if c.StyleMode != "css" && c.StyleMode != "tailwind" && c.StyleMode != "sass" && c.StyleMode != "less" {
		return errors.New("样式方案无效")
	}
	return nil
}

func copyTemplate(source fs.FS, template, target string) error {
	return fs.WalkDir(source, template, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, template)
		rel = strings.TrimPrefix(rel, "/")
		output := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(output, 0755)
		}
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return err
		}
		return os.WriteFile(output, data, 0644)
	})
}

func patchPackage(root string, config Config) error {
	path := filepath.Join(root, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		return err
	}
	pkg["name"] = config.Name
	dependencies, _ := pkg["devDependencies"].(map[string]any)
	if dependencies == nil {
		dependencies = map[string]any{}
		pkg["devDependencies"] = dependencies
	}
	scripts, _ := pkg["scripts"].(map[string]any)
	if scripts == nil {
		scripts = map[string]any{}
		pkg["scripts"] = scripts
	}
	remove := func(name string) { delete(dependencies, name) }
	if !config.UsePM2 {
		remove("pm2")
		delete(scripts, "start")
		delete(scripts, "stop")
		delete(scripts, "delete")
		_ = os.Remove(filepath.Join(root, "pm2.config.cjs"))
	}
	if config.ImageMode != "react" {
		remove("jsxp")
		_ = os.RemoveAll(filepath.Join(root, "src", "image"))
		_ = os.Remove(filepath.Join(root, "jsxp.config.tsx"))
	}
	if config.ImageMode != "react" || config.StyleMode != "tailwind" {
		remove("tailwindcss")
		remove("cssnano")
		_ = os.Remove(filepath.Join(root, "tailwind.config.js"))
		_ = os.Remove(filepath.Join(root, "postcss.config.cjs"))
	}
	if config.ImageMode == "react" && config.StyleMode == "sass" {
		dependencies["sass"] = "^1.80.0"
	}
	if config.ImageMode == "react" && config.StyleMode == "less" {
		dependencies["less"] = "^4.2.0"
	}
	if config.ESLint {
		dependencies["eslint"] = "^9.0.0"
		scripts["lint"] = "eslint ."
		_ = os.WriteFile(filepath.Join(root, "eslint.config.js"), []byte("export default [{ ignores: ['node_modules', 'dist'] }]\n"), 0644)
	}
	if !config.ESLint {
		_ = os.Remove(filepath.Join(root, "eslint.config.js"))
		delete(scripts, "lint")
	}
	encoded, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0644)
}

func run(directory string, logs *[]string, name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	line := strings.TrimSpace(string(output))
	if line != "" {
		*logs = append(*logs, line)
	}
	if err != nil {
		if os.IsPermission(err) || strings.Contains(strings.ToLower(line), "eacces") || strings.Contains(strings.ToLower(line), "permission denied") {
			elevated, elevatedErr := system.RunWithPrivileges(directory, nil, name, args...)
			if elevatedErr == nil {
				if elevated = strings.TrimSpace(elevated); elevated != "" {
					*logs = append(*logs, elevated)
				}
				return nil
			}
			if elevated != "" {
				*logs = append(*logs, strings.TrimSpace(elevated))
			}
			return fmt.Errorf("%s %s：当前用户权限不足，系统权限申请未完成：%w", name, strings.Join(args, " "), elevatedErr)
		}
		return fmt.Errorf("%s %s：%w", name, strings.Join(args, " "), err)
	}
	return nil
}

func ensureWritableDirectory(directory string) error {
	file, err := os.CreateTemp(directory, ".alemonjs-setup-write-check-")
	if err != nil {
		if os.IsPermission(err) {
			return errors.New("保存位置当前不可写，需要申请系统权限")
		}
		return fmt.Errorf("无法写入保存位置：%w", err)
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("无法确认保存位置写入权限：%w", err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("无法清理保存位置检查文件：%w", err)
	}
	return nil
}

func isPermissionError(err error) bool {
	return os.IsPermission(err) || strings.Contains(strings.ToLower(err.Error()), "permission denied") || strings.Contains(strings.ToLower(err.Error()), "eacces")
}
