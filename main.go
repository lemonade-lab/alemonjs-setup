package main

import (
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"alemonx/internal/access"
	"alemonx/internal/mcp"
	"alemonx/internal/releases"
	"alemonx/internal/robot"
	"alemonx/internal/setupplugin"
	"alemonx/internal/system"
	"alemonx/internal/web"
)

//go:embed all:dist
var staticFiles embed.FS

// 前端页面

//go:embed all:templates
var templateFiles embed.FS

// 开发模板文件 + 机器人启动目录

var Version = "dev"

func main() {
	defer robot.CleanupGitBuildSessions()
	arguments := normalizeArgs(os.Args[1:])
	port, arguments, err := option(arguments, "--port", env("PORT", "17390"))
	if err != nil {
		log.Fatal(err)
	}
	mcpPort, arguments, err := option(arguments, "--mcp-port", env("MCP_PORT", "17391"))
	if err != nil {
		log.Fatal(err)
	}
	cwd, arguments, err := option(arguments, "--cwd", ".")
	if err != nil {
		log.Fatal(err)
	}
	account, arguments, err := option(arguments, "--account", env("alx_AUTH_ACCOUNT", ""))
	if err != nil {
		log.Fatal(err)
	}
	password, arguments, err := option(arguments, "--password", env("alx_AUTH_PASSWORD", ""))
	if err != nil {
		log.Fatal(err)
	}
	confirmation, arguments, err := option(arguments, "--confirm-password", env("alx_AUTH_CONFIRM_PASSWORD", ""))
	if err != nil {
		log.Fatal(err)
	}
	yes, arguments := flagPresent(arguments, "--yes")
	if len(arguments) > 0 && (arguments[0] == "--version" || arguments[0] == "version") {
		fmt.Println(Version)
		return
	}
	if len(arguments) > 0 {
		switch arguments[0] {
		case "mcp-http":
			if len(arguments) != 1 {
				usage()
				return
			}
			token := os.Getenv("MCP_TOKEN")
			if token == "" {
				log.Fatal("请设置 MCP_TOKEN 后再启动 HTTP MCP 服务")
			}
			serveMCPHTTP(mcpPort, token, mcpPolicy())
			return
		case "mcp":
			if len(arguments) != 1 {
				usage()
				return
			}
			if err := mcp.NewServerWithPolicy(Version, templateFiles, mcpPolicy()).Serve(os.Stdin, os.Stdout); err != nil {
				log.Printf("MCP 服务已停止：%v", err)
			}
			return
		case "serve":
			if len(arguments) != 1 {
				usage()
				return
			}
		case "install":
			if len(arguments) != 1 {
				usage()
				return
			}
			result, err := system.InstallService(port)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(result)
			return
		case "open":
			if len(arguments) != 1 {
				usage()
				return
			}
			if err := system.OpenBrowser(port); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("已打开 http://127.0.0.1:%s\n", port)
			return
		case "update":
			if len(arguments) != 1 {
				usage()
				return
			}
			update, err := releases.SetupUpdate(Version)
			if err != nil {
				log.Fatal(err)
			}
			if !update.Available {
				fmt.Printf("已是最新版本：%s\n", update.Current)
				return
			}
			if !update.PlatformMatched || !update.IntegrityReady {
				fmt.Printf("发现新版本 %s，但没有可安全自动安装的匹配更新包。\n%s\n", update.Latest, update.ReleaseURL)
				return
			}
			result, err := system.ReplaceExecutable(update.DownloadURL, update.AssetName, update.SHA256, update.Latest)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(result)
			return
		case "status":
			if len(arguments) != 1 {
				usage()
				return
			}
			result, err := system.ServiceStatus()
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(result)
			return
		case "start":
			if len(arguments) != 1 {
				usage()
				return
			}
			serviceAction(system.StartService)
			return
		case "stop":
			if len(arguments) != 1 {
				usage()
				return
			}
			serviceAction(system.StopService)
			return
		case "restart":
			if len(arguments) != 1 {
				usage()
				return
			}
			serviceAction(system.RestartService)
			return
		case "uninstall":
			if len(arguments) != 1 || !yes {
				fmt.Println("请使用 alx uninstall --yes 确认移除后台服务。")
				return
			}
			serviceAction(system.UninstallService)
			return
		case "plugin":
			pluginCommand(arguments[1:], yes)
			return
		case "auth":
			authCommand(arguments[1:], yes, account, password, confirmation)
			return
		case "npm":
			if len(arguments) != 2 || arguments[1] != "publish" {
				usage()
				return
			}
			publish(cwd, "npm-publish", false)
			return
		case "git":
			if len(arguments) != 2 || arguments[1] != "publish" || !yes {
				fmt.Println("请使用 alx --cwd /项目目录 git publish --yes 确认发布。")
				return
			}
			publish(cwd, "git-release", true)
			return
		default:
			usage()
			return
		}
	}
	serve(env("alx_BIND", "127.0.0.1"), port)
}

func authCommand(arguments []string, confirmed bool, account, password, confirmation string) {
	manager, err := access.New()
	if err != nil {
		log.Fatal(err)
	}
	if len(arguments) == 1 && arguments[0] == "status" {
		status, err := manager.Status("")
		if err != nil {
			log.Fatal(err)
		}
		if status.Enabled {
			fmt.Printf("身份认证已开启，账户：%s\n", status.Account)
		} else {
			fmt.Println("身份认证未开启。")
		}
		return
	}
	if len(arguments) == 1 && arguments[0] == "enable" {
		if _, err := manager.Enable(account, password, confirmation); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("身份认证已开启，账户：%s\n", strings.TrimSpace(account))
		return
	}
	if len(arguments) == 1 && arguments[0] == "disable" {
		if !confirmed {
			fmt.Println("请使用 alx auth disable --yes 确认关闭身份认证。 ")
			return
		}
		if err := manager.Disable(); err != nil {
			log.Fatal(err)
		}
		fmt.Println("身份认证已关闭。")
		return
	}
	usage()
}

func serve(host, port string) {
	server := &http.Server{
		Addr:              host + ":" + port,
		Handler:           web.NewServer(Version, staticFiles, templateFiles),
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Print(startupMessage(Version, host, port))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// startupMessage is deliberately written for someone opening alx for the
// first time. The terminal should answer "what is this" and "what next"
// before it exposes implementation details such as the listener address.
func startupMessage(version, host, port string) string {
	addressHost := host
	if addressHost == "0.0.0.0" || addressHost == "::" {
		addressHost = "127.0.0.1"
	}
	address := "http://" + addressHost + ":" + port
	return fmt.Sprintf(`

  ALemonX v%s 已准备就绪
  ───────────────────────────────────────
  机器人工作台：

  现在开始
  1. 在浏览器打开：%s
  2. 选择「管理」→ 添加已有机器人目录
  3. 或选择「开发」→ 创建一个新的机器人项目

  接下来你还可以
  · 在「环境」检查 Node.js、Git 与包管理器
  · 在「运行」启动开发、前台或持续运行模式
  · 在「发布」打包并发布到 npm 或 Git Release
  
`, version, address)
}

func serveMCPHTTP(port, token string, policy mcp.Policy) {
	server := &http.Server{
		Addr:              "127.0.0.1:" + port,
		Handler:           mcp.NewServerWithPolicy(Version, templateFiles, policy).HTTPHandler(token),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("AlemonJS MCP HTTP 已启动：http://127.0.0.1:%s/mcp", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func mcpPolicy() mcp.Policy {
	value := strings.TrimSpace(os.Getenv("MCP_ALLOWED_ROOTS"))
	if value == "" {
		return mcp.Policy{}
	}
	roots := make([]string, 0)
	for _, root := range strings.Split(value, string(os.PathListSeparator)) {
		if root = strings.TrimSpace(root); root != "" {
			roots = append(roots, root)
		}
	}
	return mcp.Policy{AllowedRoots: roots}
}

func publish(root, action string, confirmed bool) {
	result, err := (robot.Manager{}).Run(root, action, "", "", "", "latest", "", confirmed)
	if result.Output != "" {
		fmt.Println(result.Output)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func pluginCommand(arguments []string, confirmed bool) {
	registry := setupplugin.NewRegistry()
	if len(arguments) == 1 && arguments[0] == "list" {
		items := registry.All()
		if len(items) == 0 {
			fmt.Println("暂未发现 Setup 插件。")
			return
		}
		for _, plugin := range items {
			state := "已启用"
			if !plugin.Enabled {
				state = "已卸载"
			}
			fmt.Printf("%s\tv%s\t%s\n", plugin.ID, plugin.Version, state)
		}
		return
	}
	if len(arguments) == 2 && arguments[0] == "enable" {
		if err := registry.SetEnabled(arguments[1], true); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("已启用 Setup 插件：%s\n", arguments[1])
		return
	}
	if len(arguments) == 2 && arguments[0] == "install" {
		plugin, err := registry.Install(arguments[1])
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("已安装 Setup 插件：%s v%s\n", plugin.ID, plugin.Version)
		return
	}
	if len(arguments) == 2 && arguments[0] == "disable" {
		if !confirmed {
			fmt.Printf("请使用 alx plugin disable %s --yes 确认卸载。\n", arguments[1])
			return
		}
		if err := registry.SetEnabled(arguments[1], false); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("已卸载 Setup 插件：%s；可用 alx plugin enable %s 恢复。\n", arguments[1], arguments[1])
		return
	}
	usage()
}

func serviceAction(action func() (string, error)) {
	result, err := action()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result)
}

func option(arguments []string, name, fallback string) (string, []string, error) {
	value := fallback
	remaining := make([]string, 0, len(arguments))
	for index := 0; index < len(arguments); index++ {
		if arguments[index] != name {
			remaining = append(remaining, arguments[index])
			continue
		}
		if index+1 >= len(arguments) || strings.HasPrefix(arguments[index+1], "--") {
			return "", nil, fmt.Errorf("%s 需要一个值", name)
		}
		value = arguments[index+1]
		index++
	}
	return value, remaining, nil
}

func normalizeArgs(arguments []string) []string {
	result := make([]string, len(arguments))
	for index, argument := range arguments {
		result[index] = strings.ReplaceAll(argument, "—", "--")
	}
	return result
}

func flagPresent(arguments []string, name string) (bool, []string) {
	remaining := make([]string, 0, len(arguments))
	present := false
	for _, argument := range arguments {
		if argument == name {
			present = true
			continue
		}
		remaining = append(remaining, argument)
	}
	return present, remaining
}

func usage() {
	fmt.Println(`用法:
  alx [serve] --port 17390           启动浏览器引导

  alx mcp                            启动本机 stdio MCP 服务
  MCP_TOKEN=... alx mcp-http         启动受保护的本机 HTTP MCP 服务
  alx install --port 17390           注册为后台常驻服务
  alx open [--port 17390]            打开浏览器
  alx update                         检查并更新 alx
  alx status                         查看后台服务状态
  alx start | stop | restart         管理后台服务
  alx uninstall --yes                移除后台服务
  alx plugin list                     查看已发现的 Setup 插件
  alx plugin install <id>             在线安装一个 Setup 插件
  alx plugin disable <id> --yes       卸载（停用）一个 Setup 插件
  alx plugin enable <id>              重新启用一个 Setup 插件

  alx auth status                      查看身份认证状态
  alx auth enable --account <账户> --password <密码> --confirm-password <确认密码>
                                     开启本机身份认证（也支持 alx_AUTH_* 环境变量注入）
  alx auth disable --yes               关闭身份认证
  alx [--cwd /项目目录] npm publish  发布到 npm 官方仓库
  alx [--cwd /项目目录] git publish --yes  创建 GitHub Release 标签`)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
