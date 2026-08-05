package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"alemonjs-setup/internal/mcp"
	"alemonjs-setup/internal/project"
	"alemonjs-setup/internal/releases"
	"alemonjs-setup/internal/robot"
	"alemonjs-setup/internal/setupplugin"
	"alemonjs-setup/internal/system"
	"alemonjs-setup/internal/web"
)

//go:embed all:dist
var staticFiles embed.FS

// 前端页面

//go:embed all:templates
var templateFiles embed.FS

// 开发模板文件 + 机器人启动目录

var Version = "dev"

func main() {
	if len(os.Args) == 4 && os.Args[1] == "--privileged-create" {
		privilegedCreate(os.Args[2], os.Args[3])
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "--privileged-robot" {
		if err := robot.ExecutePrivilegedRequest(os.Args[2], os.Args[3]); err != nil {
			log.Printf("执行权限操作失败：%v", err)
		}
		return
	}
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
			if !update.PlatformMatched {
				fmt.Printf("发现新版本 %s，但未找到当前系统的安装包。\n%s\n", update.Latest, update.ReleaseURL)
				return
			}
			result, err := system.ReplaceExecutable(update.DownloadURL, update.AssetName)
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
				fmt.Println("请使用 albs uninstall --yes 确认移除后台服务。")
				return
			}
			serviceAction(system.UninstallService)
			return
		case "plugin":
			pluginCommand(arguments[1:], yes)
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
				fmt.Println("请使用 albs --cwd /项目目录 git publish --yes 确认发布。")
				return
			}
			publish(cwd, "git-release", true)
			return
		default:
			usage()
			return
		}
	}
	serve(port)
}

func privilegedCreate(configPath, resultPath string) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Printf("读取创建请求失败：%v", err)
		return
	}
	var config project.Config
	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("创建请求无效：%v", err)
		return
	}
	result, createErr := project.NewCreator(templateFiles).Create(config)
	response := struct {
		Result project.Result `json:"result"`
		Error  string         `json:"error,omitempty"`
	}{Result: result}
	if createErr != nil {
		response.Error = createErr.Error()
	}
	data, err = json.Marshal(response)
	if err == nil {
		err = os.WriteFile(resultPath, data, 0666)
	}
	if err != nil {
		log.Printf("写入创建结果失败：%v", err)
	}
}

func serve(port string) {
	server := &http.Server{
		Addr:              "127.0.0.1:" + port,
		Handler:           web.NewServer(Version, staticFiles, templateFiles),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("alemonjs-setup %s 已启动：http://localhost:%s", Version, port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
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
	if len(arguments) == 2 && arguments[0] == "disable" {
		if !confirmed {
			fmt.Printf("请使用 albs plugin disable %s --yes 确认卸载。\n", arguments[1])
			return
		}
		if err := registry.SetEnabled(arguments[1], false); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("已卸载 Setup 插件：%s；可用 albs plugin enable %s 恢复。\n", arguments[1], arguments[1])
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
  albs [serve] --port 17390           启动浏览器引导

  albs mcp                            启动本机 stdio MCP 服务
  MCP_TOKEN=... albs mcp-http         启动受保护的本机 HTTP MCP 服务
  albs install --port 17390           注册为后台常驻服务
  albs open [--port 17390]            打开浏览器
  albs update                         检查并更新 albs
  albs status                         查看后台服务状态
  albs start | stop | restart         管理后台服务
  albs uninstall --yes                移除后台服务
  albs plugin list                     查看已发现的 Setup 插件
  albs plugin disable <id> --yes       卸载（停用）一个 Setup 插件
  albs plugin enable <id>              重新启用一个 Setup 插件
  albs [--cwd /项目目录] npm publish  发布到 npm 官方仓库
  albs [--cwd /项目目录] git publish --yes  创建 GitHub Release 标签`)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
