# ALemonX（`alx`）

ALemonX 是 AlemonJS 机器人的本地管理台。它把环境检查、创建或导入机器人、运行、日志、插件、连接、Git 与发布整理到浏览器界面中；不需要记忆命令，也不提供可输入的网页终端。

它只监听本机 `127.0.0.1`，不会把机器人管理台暴露到局域网或公网。

## 我能用它做什么？

- 用引导创建开发机器人，或用推荐默认值快速安装机器人。
- 导入本地机器人目录，或从 GitHub / Gitee 克隆机器人仓库。
- 配置机器人、连接平台、管理 `packages/` 背包与本地插件。
- 启动开发运行、前台运行或 PM2 持久化运行。
- 查看只读运行日志和可翻页的 PM2 日志。
- 安装机器人插件、连接包和 Setup 系统插件。
- 通过 Git 打包或发布到 npm。
- 供 Codex 等 MCP 客户端在明确确认后执行受控项目操作。

## 给使用者：下载并开始

在 [GitHub Releases](https://github.com/lemonade-lab/alemonx/releases) 下载最新版本。Release 提供的是压缩包，解压后得到 `alx`（Windows 为 `alx.exe`）。

| 系统 | 下载文件 |
| --- | --- |
| Windows 64 位 | `alx-windows-amd64.zip` |
| macOS Apple Silicon | `alx-darwin-arm64.zip` |
| macOS Intel | `alx-darwin-amd64.zip` |
| Linux 64 位 | `alx-linux-amd64.zip` |

Windows 直接双击 `alx.exe`。macOS / Linux 在解压目录运行：

```bash
chmod +x alx
./alx
```

终端会显示本地地址，例如 `http://127.0.0.1:17390`。在浏览器打开它，即会进入“你现在想做什么？”引导页。

首次使用可从以下三种目的选择：

1. **开发**：逐步选择语言、包管理器、Git、图片开发与开发技能，再创建项目。
2. **部署**：安装源码机器人、桌面版、手机版或 Web 版。涉及 GitHub Release 的流程会先选镜像、版本和安装包，并把当前系统/架构推荐置顶。
3. **管理**：进入后台中心，添加已有机器人目录或从 Git 仓库克隆。

下载页会显示“打开 GitHub 发布页”；应用右上角的更新按钮也会只推荐与当前系统和架构精确匹配的安装包。

## 后台中心

选中机器人目录后，后台中心按实际使用顺序提供功能。

| 功能 | 说明 |
| --- | --- |
| 配置 | 可视化编辑 AlemonJS 配置；开发模式可切换纯文本、`.npmrc` 与 `.env`。 |
| 运行 | 检查/重载依赖，启动和停止前台或开发运行，管理 PM2。 |
| 连接 | 按在线文档安装平台连接包并填写扩展配置。 |
| 背包 | 管理本地 `packages/` 中的插件包及其版本、配置。 |
| 插件 | 从在线目录安装机器人插件；带 `alemonjs.web.root` 的插件会在当前机器人卡片下注册 Web 页面入口。 |
| 发布 | 开发模式下管理包信息、Git 打包与 npm 发布。 |

### 运行、日志与 PM2

无论开发运行、前台运行还是 PM2 持久化运行，启动前都会自动检查：

- 是否存在 `node_modules`；
- `package.json` 中的直接 `dependencies` 与 `devDependencies` 是否确实安装；
- 已选择连接的必填配置是否完整。

检查不通过会阻止启动，并提示先点“重载依赖”。

“查看运行日志”是只读窗口：用于查看开发运行和前台运行的实时输出，不允许输入命令。PM2 提供状态、日志、停止、重启、平滑重载、删除和修复配置；PM2 日志默认展示最新 120 行，并可向前翻页查看更早日志。

### Git 与 SSH

机器人目录支持两种来源：选择本地文件夹，或填写 GitHub / Gitee HTTPS 仓库地址并选择存放位置、最终目录名和下载镜像。克隆前会检查目标目录是否已存在。

窗口左上角的 SSH 管理仅展示公钥，不读取或展示私钥。没有公钥时可以生成 Ed25519 密钥；复制后可直接打开：

- [GitHub SSH 公钥设置](https://github.com/settings/keys) 与 [GitHub 官方教程](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/adding-a-new-ssh-key-to-your-github-account)
- [Gitee SSH 公钥设置](https://gitee.com/profile/sshkeys) 与 [Gitee 官方教程](https://gitee.com/help/articles/4181)

## 身份认证与数据安全

后台中心左上角可开启身份认证。开启后，管理 API 需先登录；密码只以 bcrypt 哈希保存在当前用户的配置目录，浏览器登录态为仅本机可用的 HttpOnly Cookie。

为了避免浏览器留下凭据，`.env`、`.npmrc`、`alemon.config.yaml` / `.yml` 的未保存草稿不会写入 `localStorage`，刷新页面会从项目文件重新读取。

也可用 CLI 配置认证：

```bash
alx auth enable --account lemonade --password 'your-password' --confirm-password 'your-password'
alx auth status
alx auth disable --yes
```

自动化脚本可改用 `alx_AUTH_ACCOUNT`、`alx_AUTH_PASSWORD` 与 `alx_AUTH_CONFIRM_PASSWORD`，避免把密码写入命令历史。

## CLI

将 `alx` 放入 `PATH` 后，可选地把管理台注册为登录后常驻服务：

```bash
alx install --port 17390
alx open
alx status
alx start
alx stop
alx restart
alx update
alx uninstall --yes
```

项目发布也可以自动化执行：

```bash
alx --cwd /path/to/robot npm publish
alx --cwd /path/to/robot git publish --yes
```

## MCP：让 AI 助手协助管理机器人

`alx` 支持标准 MCP。它不提供任意 shell、任意路径读写或密钥读取；写入文件、安装依赖、启动、构建、打包与发布均要求客户端显式确认。

**STDIO（推荐 Codex 与桌面客户端）**：

```json
{
  "mcpServers": {
    "alemonx": {
      "command": "alx",
      "args": ["mcp"]
    }
  }
}
```

**Streamable HTTP**：

```bash
MCP_TOKEN='高强度随机值' alx --mcp-port 17391 mcp-http
```

填写地址 `http://127.0.0.1:17391/mcp`，认证填写 `Bearer <MCP_TOKEN>`。完整控制边界与任务约定见 [MCP 控制面文档](docs/mcp.md)。

## 本地开发

前置要求：Go 1.23+、Node.js 22+、Yarn 1.x。

```bash
# 终端一：Go API
go run .

# 终端二：Vite 前端
cd frontend
yarn install
yarn dev
```

- 前端：`http://localhost:5173`
- Go API：`http://localhost:17390`

常用校验命令：

```bash
make build-fe   # 构建前端到 dist/
make test       # Go 测试
make lint       # go vet
make build      # 构建嵌入前端与模板的单文件

cd frontend && yarn lint && yarn build
```

## 仓库结构

```text
frontend/    React + Vite 管理台与引导
internal/    HTTP API、环境检查、机器人、发布、MCP 与系统服务
templates/   嵌入二进制的 JS / TS AlemonJS 项目模板
plugins/     Setup 系统插件及其可选执行器
docs/        设计与 MCP 文档
.github/     CI 与跨平台 Release 工作流
```

## 维护与发布

- 机器人目录必须是包含 `package.json` 的本地 Node.js 项目。
- 安装插件与连接包经过后端白名单，不开放浏览器任意命令执行。
- 系统插件与机器人插件必须隔离：系统插件增强 Setup，机器人插件只影响选中的项目。
- 开发与接入 Setup 系统插件请参阅 [系统插件开发文档](docs/setup-plugin-development.md)。
- 修改前端后运行 `yarn --cwd frontend lint && yarn --cwd frontend build`；修改 Go 后运行 `go test ./internal/... && go vet ./internal/...`。

推送 `v*` 标签会触发 GitHub Actions，构建 Windows、macOS（Apple Silicon / Intel）和 Linux 压缩包，并创建 GitHub Release：

```bash
git tag v0.1.0
git push origin v0.1.0
```
