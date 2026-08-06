# AlemonJS Setup

构建一个本地 Web 控制中心，用于创建、发现和维护多个 AlemonJS 机器人项目。

仓库面向三类读者：

- 使用者：下载已构建的桌面二进制，管理本机机器人目录。
- 开发者：运行前后端、修改控制中心或项目模板。
- 维护者：构建跨平台二进制、验证变更和发布版本。

## 获取与运行

在 [Releases](../../releases) 下载对应系统的二进制：

| 系统 | 文件 |
| --- | --- |
| Windows | `alemonjs-setup-windows-amd64.exe` |
| macOS Apple Silicon | `alemonjs-setup-darwin-arm64` |
| macOS Intel | `alemonjs-setup-darwin-amd64` |
| Linux | `alemonjs-setup-linux-amd64` |


Windows 可直接双击 `.exe`。

其他系统，运行二进制后，访问终端显示的地址；默认是 `http://localhost:17390`。

```bash
./alemonjs-setup-darwin-arm64 install --port 17390
```

如果安装输出提示 `~/.local/bin` 不在 `PATH` 中，请先按终端提示加入 `PATH`，再重新打开终端。完成后直接使用 `albs` 管理本机服务或自动化发布：

```bash
albs open
albs status
albs start
albs stop
albs restart
albs uninstall --yes
albs --cwd /xxx/robot npm publish
albs --cwd /xxx/alemonb git publish --yes
```

## 本机身份认证

后台中心左上角可开启身份认证：填写账户、密码和确认密码后，所有管理 API 都必须先登录。密码只以 bcrypt 哈希保存到当前登录用户的配置目录，登录态是仅限本机浏览器的 HttpOnly Cookie。

也可在终端配置，适合安装脚本注入账户信息：

```bash
albs auth enable --account lemonade --password 'your-password' --confirm-password 'your-password'
albs auth status
albs auth disable --yes
```

自动化脚本可改用 `ALBS_AUTH_ACCOUNT`、`ALBS_AUTH_PASSWORD` 和 `ALBS_AUTH_CONFIRM_PASSWORD` 环境变量，避免将密码写入命令历史。

## 让 AI 助手管理本机机器人（MCP）

AlemonJS Setup 支持 MCP 的两种标准接入方式，因此可由 Codex、豆包或其它支持标准 MCP 工具调用的客户端控制。它提供项目检查、源码读写、运行配置、本地包、机器人启动/停止、构建、打包与发布等受控操作。

**STDIO（推荐给 Codex 和桌面客户端）**：客户端启动 `albs` 子进程，无需网络端口。

```json
{
  "mcpServers": {
    "alemonjs-setup": {
      "command": "albs",
      "args": ["mcp"]
    }
  }
}
```

**Streamable HTTP（适合提供“URL + Token”图形表单的客户端）**：

```bash
MCP_TOKEN='生成的高强度随机值' albs --mcp-port 17391 mcp-http
```

填写地址 `http://127.0.0.1:17391/mcp`，认证填写 `Bearer <MCP_TOKEN>`。服务只监听本机 `127.0.0.1`，不可暴露到局域网或公网。

使用前请确认 `albs` 已安装且可从客户端启动环境找到：`command -v albs`。Codex 的图形表单分别选择 `STDIO` 或 `流式 HTTP`；豆包及其它客户端只要提供标准 MCP 的命令配置或 URL/Token 配置，可填写相同字段。各产品的授权弹窗和 UI 名称可能不同，但不能跳过服务端的控制边界。

服务不提供任意 shell 或任意路径读写：`.env`、`.npmrc`、密钥文件、Git 元数据、依赖目录和符号链接均不可访问。会写入文件、安装依赖、启动机器人、构建或发布的工具都要求客户端在用户明确同意后传 `confirm: true`。

控制范围、任务轮询与未来远程接入的约定见 [MCP 控制面文档](docs/mcp.md)。


## 本地开发

前置要求：Go 1.23+、Node.js 22+、Yarn 1.x。

```bash
# 终端一：启动 Go API
go run .

# 终端二：启动 Vite 前端
cd frontend
yarn install
yarn dev
```

- 前端：`http://localhost:5173`
- Go 服务：`http://localhost:17390`

## 常用命令

```bash
make build-fe  # 构建前端到 dist/
make test      # 运行 Go 测试
make lint      # 检查前端代码
make build     # 嵌入前端和模板，构建最终单文件
```

前端也可单独验证：

```bash
cd frontend
yarn build
```

## 仓库结构

```text
frontend/    React + Vite 控制中心
internal/    HTTP API、环境检查、项目创建、目录及发布管理
templates/   嵌入二进制的 JS / TS AlemonJS 项目模板
scripts/     本地发布与维护脚本
.github/     CI 与跨平台发布工作流
```

## 维护约定

- 机器人操作只接受包含 `package.json` 的本地 Node.js 项目目录。
- 安装机器人插件和连接包时必须使用后端白名单，不能开放任意命令执行。
- 系统能力与单个机器人目录能力应保持独立：前者不得隐式修改机器人项目。
- 修改前端后运行 `yarn build`；修改 Go 代码后运行 `go test ./...`。

## 发布

推送 `v*` 标签会触发 GitHub Actions，构建 Windows、macOS（Apple Silicon / Intel）和 Linux 二进制，并创建 GitHub Release。

```bash
git tag v0.1.0
git push origin v0.1.0
```
