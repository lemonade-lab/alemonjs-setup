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
	Template            string   `json:"template"`
	Name                string   `json:"name"`
	DestinationMode     string   `json:"destinationMode"`
	Destination         string   `json:"destination"`
	Language            string   `json:"language"`
	PackageManager      string   `json:"packageManager"`
	ESLint              bool     `json:"eslint"`
	InitializeGit       bool     `json:"initializeGit"`
	UsePM2              bool     `json:"usePM2"`
	ImageMode           string   `json:"imageMode"`
	StyleMode           string   `json:"styleMode"`
	DownloadSkills      bool     `json:"downloadSkills"`
	DevelopmentPackages []string `json:"developmentPackages"`
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
			return Result{}, permissionAdvice("访问所选保存位置")
		}
		return Result{}, errors.New("保存位置不存在或不是文件夹，请重新选择")
	} else if !info.IsDir() {
		return Result{}, errors.New("保存位置不存在或不是文件夹，请重新选择")
	}
	if err := ensureWritableDirectory(config.Destination); err != nil {
		if isPermissionError(err) {
			return Result{}, permissionAdvice("在所选保存位置创建项目")
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
	if err := patchDevelopmentSource(path, config); err != nil {
		return result, fmt.Errorf("调整开发模板失败：%w", err)
	}
	if err := writeAgentsFile(path, config); err != nil {
		return result, fmt.Errorf("写入开发约定失败：%w", err)
	}
	log("已按你的选择配置项目。")

	packageCommand := config.PackageManager
	packageName := ""
	if _, err := exec.LookPath(config.PackageManager); err != nil {
		// Do not try `npm install --global`: that commonly needs administrator
		// rights. npx is scoped to this one command and keeps a new machine
		// usable without modifying its global package directory.
		switch config.PackageManager {
		case "yarn":
			packageName = "yarn@1.22.22"
		case "pnpm":
			packageName = "pnpm@latest"
		}
		if packageName != "" {
			log("未找到 " + strings.ToUpper(config.PackageManager) + "，临时使用 npm 下载并执行；不会修改电脑的全局安装。")
			packageCommand = "npx"
		}
	}
	log("正在安装项目依赖…")
	install := map[string][]string{"yarn": {"install"}, "npm": {"install"}, "pnpm": {"install"}}[config.PackageManager]
	if packageCommand == "npx" {
		install = append([]string{"--yes", packageName}, install...)
	}
	if err := run(path, &result.Logs, packageCommand, install...); err != nil {
		if packageCommand == "npx" {
			return result, fmt.Errorf("临时使用 %s 安装依赖失败；请确认 Node.js 已安装，或返回“包管理器”步骤选择 npm 后重试：%w", strings.ToUpper(config.PackageManager), err)
		}
		return result, fmt.Errorf("安装项目依赖失败：%w", err)
	}

	if config.InitializeGit {
		log("正在初始化 Git 存档…")
		for _, command := range [][]string{{"init"}, {"config", "user.name", "ALemonX"}, {"config", "user.email", "setup@alemonjs.local"}, {"add", "."}, {"commit", "-m", "chore: initialize alemonjs project"}} {
			if err := run(path, &result.Logs, "git", command...); err != nil {
				return result, fmt.Errorf("初始化 Git 失败：%w", err)
			}
		}
	}
	if config.DownloadSkills {
		log("正在下载 AlemonJS 开发技能…")
		// The canonical location is the cross-agent .agents/skills directory
		// (agentskills.io spec; Codex/OpenCode/Gemini read it natively). Claude
		// Code only reads .claude/skills, so link the same checkout in there
		// instead of keeping a second copy. A custom top-level .skills/ is read
		// by no agent and is therefore useless.
		if err := run(path, &result.Logs, "git", "clone", "--depth", "1", "https://github.com/lemonade-lab/alemonjs-dev-skill.git", ".agents/skills/alemonjs-dev-skill"); err != nil {
			return result, fmt.Errorf("下载开发技能失败：%w", err)
		}
		if err := os.MkdirAll(filepath.Join(path, ".claude", "skills"), 0755); err != nil {
			return result, fmt.Errorf("创建 Claude 技能目录失败：%w", err)
		}
		if err := os.Symlink(filepath.Join("..", "..", ".agents", "skills", "alemonjs-dev-skill"), filepath.Join(path, ".claude", "skills", "alemonjs-dev-skill")); err != nil {
			return result, fmt.Errorf("创建 Claude 技能链接失败：%w", err)
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
	for _, capability := range c.DevelopmentPackages {
		if _, ok := developmentPackageCapabilities[capability]; !ok {
			return errors.New("开发能力包无效")
		}
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
	// Persist the user's package-manager choice. Subsequent robot operations
	// read package.json first, so a copied project behaves consistently even
	// before a lock file exists.
	if config.PackageManager != "" {
		pkg["packageManager"] = map[string]string{"npm": "npm@10", "yarn": "yarn@1.22.22", "pnpm": "pnpm@9.15.0"}[config.PackageManager]
	}
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
	for _, dependency := range developmentPackageCapabilities {
		remove(dependency)
	}
	for _, capability := range config.DevelopmentPackages {
		dependencies[developmentPackageCapabilities[capability]] = developmentPackageVersions[capability]
	}
	if !config.UsePM2 {
		remove("pm2")
		delete(scripts, "start")
		delete(scripts, "stop")
		delete(scripts, "delete")
		_ = os.Remove(filepath.Join(root, "pm2.config.cjs"))
	}
	if config.UsePM2 {
		dependencies["pm2"] = "^5"
		dependencies["yaml"] = "^2.6.0"
	}
	if config.ImageMode != "react" {
		remove("jsxp")
		_ = os.RemoveAll(filepath.Join(root, "src", "image"))
		_ = os.Remove(filepath.Join(root, "jsxp.config.tsx"))
	}
	if config.ImageMode == "react" {
		dependencies["jsxp"] = "1.4.0"
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
	if config.Language == "js" {
		remove("@types/node")
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

var developmentPackageCapabilities = map[string]string{
	"bubble":   "@alemonjs/bubble",
	"database": "@alemonjs/db",
	"discord":  "@alemonjs/discord",
	"onebot":   "@alemonjs/onebot",
	"qqbot":    "@alemonjs/qq-bot",
}

var developmentPackageVersions = map[string]string{
	"bubble":   "^2.1.11",
	"database": "^0.0.17",
	"discord":  "^2.1.23",
	"onebot":   "^2.1.16",
	"qqbot":    "^2.1.23",
}

// writeAgentsFile writes an AGENTS.md that documents the project's conventions
// so coding agents (built-in or external) follow them. It reflects the chosen
// language and whether ESLint was enabled.
func writeAgentsFile(root string, config Config) error {
	extension := "ts"
	if config.Language == "js" {
		extension = "js"
	}
	verify := "npx tsc --noEmit"
	if config.ESLint {
		verify += "\nnpx eslint src --ext ." + extension
	}
	content := `# AGENTS.md

这是一个 AlemonJS（聊天平台机器人开发框架）项目，基于 Node.js 开发。

## 工作原则

- 在开始修改前，先查看 package.json、相关源码目录 src 和已有实现方式。
- 优先做最小改动，只修改完成当前任务所必需的文件。
- 优先复用现有模块、工具函数和数据结构，不要随意新增依赖。
- 不要无故重构、重命名或移动文件；不要修改与当前任务无关的代码。
- 修改代码时，保持现有项目结构、编码风格和运行方式一致。

## 验证

改完代码后运行项目验证命令，确认没有破坏现有功能：

` + "```" + `bash
` + verify + `
` + "```" + `

如果由于环境、依赖、权限或上下文限制无法执行，请明确说明原因，不要假装已经完成验证。

## 项目目录说明

- src/：项目源码目录
- lvy.config.ts：基于 tsx 和 rollup 的开发工具配置，修改代码时需要符合其中的构建与运行约定
`
	return os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(content), 0644)
}

func patchDevelopmentSource(root string, config Config) error {
	if config.Template != "dev" {
		return nil
	}
	extension := "ts"
	if config.Language == "js" {
		extension = "js"
	}
	// Keep only the variant matching the chosen language. The template ships
	// both app.ts/app.js, src/index.ts/src/index.js, … as hand-maintained
	// counterparts; the unused one is removed so the generated project never
	// mixes extensions or keeps TypeScript syntax in a JavaScript project.
	for _, pair := range [][2]string{
		{"app.ts", "app.js"},
		{"lvy.config.ts", "lvy.config.js"},
		{"jsxp.config.tsx", "jsxp.config.jsx"},
		{"src/index.ts", "src/index.js"},
		{"src/expose.ts", "src/expose.js"},
		{"src/store.ts", "src/store.js"},
		{"src/response/hello.ts", "src/response/hello.js"},
		{"src/response/help.ts", "src/response/help.js"},
		{"src/image/component/Html.tsx", "src/image/component/Html.jsx"},
		{"src/image/component/help.tsx", "src/image/component/help.jsx"},
	} {
		keep, drop := pair[0], pair[1]
		if config.Language == "js" {
			keep, drop = pair[1], pair[0]
		}
		if err := os.Remove(filepath.Join(root, drop)); err != nil && !os.IsNotExist(err) {
			return err
		}
		if _, err := os.Stat(filepath.Join(root, keep)); err != nil {
			// A non-React project has no jsxp.config / image components; those
			// pairs are optional and may legitimately be absent after patchPackage.
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
	}
	// TypeScript-only ambient declarations are not needed by JS projects.
	if config.Language == "js" {
		_ = os.Remove(filepath.Join(root, "src", "env.d.ts"))
	}
	if config.ImageMode != "react" {
		help := "import { Format, useMessage } from 'alemonjs';\n\nexport default async () => {\n  const [message] = useMessage();\n  await message.send({ format: Format.create().addText('AlemonJS 开发机器人已就绪。') });\n};\n"
		if err := os.WriteFile(filepath.Join(root, "src", "response", "help."+extension), []byte(help), 0644); err != nil {
			return err
		}
	}
	packagePath := filepath.Join(root, "package.json")
	packageData, err := os.ReadFile(packagePath)
	if err != nil {
		return err
	}
	var pkg map[string]any
	if err := json.Unmarshal(packageData, &pkg); err != nil {
		return err
	}
	scripts, _ := pkg["scripts"].(map[string]any)
	if scripts != nil && config.Language == "js" {
		scripts["dev"] = "lvy app.js"
		scripts["view"] = "lvy app.js --jsxp"
	}
	encoded, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(packagePath, append(encoded, '\n'), 0644)
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
		if isPermissionError(err) || isPermissionError(errors.New(line)) {
			return permissionAdvice("执行 " + filepath.Base(name))
		}
		if commandNotFound(err, line) {
			return missingCommandAdvice(filepath.Base(name))
		}
		if line != "" {
			return fmt.Errorf("%s：%w", line, err)
		}
		return fmt.Errorf("%s %s：%w", name, strings.Join(args, " "), err)
	}
	return nil
}

func ensureWritableDirectory(directory string) error {
	file, err := os.CreateTemp(directory, ".alemonx-write-check-")
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

func permissionAdvice(action string) error {
	return fmt.Errorf("没有权限%s。请在系统设置中为 alx 授予该磁盘或文件夹的访问权限，或选择当前登录账户可读写的保存位置后重试", action)
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
		return errors.New("未检测到 Node.js 运行环境（含 npm/npx）。请先在左上角“环境”中安装 Node.js LTS，完成后重新创建项目")
	case "git":
		return errors.New("未检测到 Git。请先在左上角“环境”中安装 Git，完成后重新创建项目")
	default:
		return fmt.Errorf("未检测到 %s 命令。请安装对应的系统工具后重新创建项目", name)
	}
}
