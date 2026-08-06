# AlemonJS Setup MCP 控制面

AlemonJS Setup 同时支持 MCP 的两种标准传输：**stdio** 和 **Streamable HTTP**。两者均使用 JSON-RPC 2.0，向 Codex、豆包等本机 AI 客户端开放同一组受限的 Setup 控制工具。

## 客户端兼容性与验证范围

| 客户端类别 | 推荐方式 | 本项目的保证 | 客户端侧仍需完成 |
| --- | --- | --- | --- |
| Codex 桌面端 | STDIO；也可用流式 HTTP 表单 | 覆盖 `initialize`、`notifications/initialized`、工具发现、调用、资源发现/读取及两种传输 | 确保 `albs` 在 Codex 可启动的 PATH；HTTP 时填写 Token。 |
| 豆包等桌面 Agent | 该产品提供的标准 MCP 命令或 HTTP 表单 | 与 Codex 使用相同 JSON-RPC、工具和权限边界 | 在各自的 MCP 设置中选择 STDIO 或流式 HTTP；产品的授权 UI 由客户端负责。 |
| 其它 MCP 客户端 | STDIO 或 Streamable HTTP | 不依赖厂商私有 API；工具 schema、annotations、文本结果与 structuredContent 均为标准 MCP 数据 | 客户端必须支持 MCP 2025-06-18 或兼容版本，并展示真实的用户确认。 |

“支持”指协议互操作，不表示任一第三方客户端会绕过自身的账号、管理员策略、网络策略或人工确认机制。启动前可用 `command -v albs` 确认 STDIO 客户端能够找到二进制文件。

## 连接方式

### STDIO（推荐）

`albs mcp` 由 AI 客户端作为本机子进程启动，不复用浏览器会话，也不监听网络端口。在 Codex 的“连接至自定义 MCP”表单中填写：

| 字段 | 值 |
| --- | --- |
| 名称 | `alemonjs-setup` |
| 类型 | `STDIO` |
| 启动命令 | `albs` |
| 参数 | `mcp` |
| 环境变量（可选） | `MCP_ALLOWED_ROOTS=/你的/机器人目录` |

### 流式 HTTP

对于使用图形化“流式 HTTP”表单的客户端，先设置随机的 `MCP_TOKEN` 并启动受保护的 loopback 服务：

```bash
MCP_TOKEN='生成的高强度随机值' albs --mcp-port 17391 mcp-http
```

在表单中填写：

| 字段 | 值 |
| --- | --- |
| 名称 | `alemonjs-setup` |
| 类型 | `流式 HTTP` |
| 地址 | `http://127.0.0.1:17391/mcp` |
| 认证 | `Bearer <MCP_TOKEN>` |

端点实现 Streamable HTTP 的 `POST /mcp` 请求/响应模式；独立 SSE 推送流不适用于此控制面，会按规范以 `405` 拒绝 `GET /mcp`。它校验 `Origin`、协议版本与 Bearer Token，且绝不监听局域网或公网地址。

可选地用 `MCP_ALLOWED_ROOTS` 限制 Agent 只能管理特定工作区；多个路径以操作系统的路径分隔符连接（macOS/Linux 使用 `:`，Windows 使用 `;`）：

```bash
MCP_ALLOWED_ROOTS='/Users/me/robots:/Users/me/workspaces' albs mcp
```

一旦配置，项目读写、任务操作与新项目创建都会在服务端验证目录是否位于这些根路径内。

## 能力模型

| 层级 | MCP 能力 | 用途 |
| --- | --- | --- |
| 上下文 | `resources/list`、`resources/read` | 获取 `alemonjs://mcp/capabilities`，先了解可用范围与边界。 |
| 系统检查 | 环境、更新、版本与官方生态目录 | 让 Agent 在创建、安装或发布前取得当前事实。 |
| 只读 | 项目状态、文件列表、读取源码、本地包列表 | 让 Agent 先检查，再决定是否需要修改。 |
| 修改 | 写入项目文件、创建项目 | 每次调用都必须带 `confirm: true`。 |
| 异步操作 | 启动项目操作、查询任务、列出任务 | 安装、构建、Git 操作不会阻塞 MCP 连接。 |

所有工具同时提供文本结果和 `structuredContent`，因此客户端既可向模型展示结果，也可稳定读取字段。

## 与 Setup 机器人管理的对应关系

| Setup 能力 | MCP 工具 |
| --- | --- |
| 依赖检查、安装、构建 | `alemonjs_project_status`、`alemonjs_start_project_action`（`install`、`build`） |
| 开发与后台机器人生命周期 | `alemonjs_start_project_action`（`dev`、`pm2`、`pm2-status`、`pm2-stop`）及 `alemonjs_stop_development` |
| 运行配置 | `alemonjs_get_package_runtime_config`、`alemonjs_save_package_runtime_config` |
| 本地包与安装包 | `alemonjs_list_local_packages`、`alemonjs_start_project_action`（`install-package`、`uninstall-package`） |
| 包发布信息 | `alemonjs_get_package_manifest`、`alemonjs_save_package_manifest` |
| NPM 打包与发布 | `alemonjs_get_npm_publish_status`、`alemonjs_get_npm_pack_preview`、`alemonjs_start_project_action`（`npm-version`、`npm-publish`） |
| Git 初始化与打包 | `alemonjs_initialize_git`、`alemonjs_get_git_release_status`、`alemonjs_start_project_action`（`commit`、`git-release`） |
| Setup 系统扩展 | `alemonjs_list_setup_plugins`、`alemonjs_run_setup_plugin` |
| Setup 系统检查 | `alemonjs_check_environment`、`alemonjs_check_setup_update`、`alemonjs_list_releases`、`alemonjs_list_catalog` |

`npm-publish` 与 `git-release` 会产生外部副作用，必须在发布前检查之后得到用户本次明确确认；MCP 不读取或传递 npm token。

## 推荐的 Agent 工作流

1. 读取 `alemonjs://mcp/capabilities`。
2. 用 `alemonjs_project_status`、`alemonjs_list_project_files` 了解目标项目。
3. 读取必要的源码文件，提出修改和影响说明。
4. 得到用户确认后，以 `confirm: true` 写入文件或调用 `alemonjs_start_project_action`。
5. 对长操作使用 `alemonjs_get_project_task` 轮询到 `completed` 或 `failed`。

## 权限边界

项目根目录必须包含 `package.json`。MCP 允许管理项目源码与普通配置，但永久拒绝：

- 任意宿主机 Shell 命令；
- `.env`、`.npmrc`、私钥/证书文件；
- `.git`、`node_modules`、符号链接；
- 超过 1 MiB 的文件读取或写入。

开发模式、NPM 发布、GIT 发布和 Setup 插件操作均经过显式工具与 `confirm: true` 控制；其中 NPM 发布和 GIT 发布还应在对应的预检工具通过后再执行。确认字段由 MCP 客户端发出，因此客户端必须提供真实的用户确认界面，不能将它视为独立的授权系统。

这些约束位于 Go 服务层，而不是依赖客户端提示。设置 `MCP_ALLOWED_ROOTS` 时，服务端会先解析真实路径再比较边界，避免符号链接绕过。未来如需接入远程 MCP，应将 HTTP adapter 放在带 OAuth、项目范围策略、审计日志和速率限制的网关之后；不能直接把当前本机控制器暴露到公网。

协议实现遵循 [MCP 2025-06-18 工具规范](https://modelcontextprotocol.io/specification/2025-06-18/server/tools)：声明工具能力、工具元数据和结构化结果，并保持用户对修改性操作的确认权。
