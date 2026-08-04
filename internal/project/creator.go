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
	if info, err := os.Stat(config.Destination); err != nil || !info.IsDir() {
		return Result{}, errors.New("保存位置不存在或不是文件夹，请重新选择")
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

	if config.PackageManager == "yarn" {
		if _, err := exec.LookPath("yarn"); err != nil {
			log("未找到 Yarn，正在通过 npm 安装 Yarn…")
			if err := run(path, &result.Logs, "npm", "install", "--global", "yarn"); err != nil {
				return result, fmt.Errorf("安装 Yarn 失败：%w", err)
			}
		}
	}
	log("正在安装项目依赖…")
	install := map[string][]string{"yarn": {"install"}, "npm": {"install"}, "pnpm": {"install"}}[config.PackageManager]
	if err := run(path, &result.Logs, config.PackageManager, install...); err != nil {
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
		return fmt.Errorf("%s %s：%w", name, strings.Join(args, " "), err)
	}
	return nil
}
