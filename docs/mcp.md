# AlemonJS Setup MCP 控制面

`albs mcp` 是面向 Codex、豆包等本机 AI 客户端的 stdio MCP Server。它不复用浏览器会话，也不监听网络端口；客户端启动进程后通过 JSON-RPC 2.0 交换消息。

对于仅支持 HTTP 的本机客户端，可设置随机的 `MCP_TOKEN` 后启动受保护的 loopback adapter：

```bash
MCP_TOKEN='生成的高强度随机值' albs --mcp-port 17391 mcp-http
```

端点是 `http://127.0.0.1:17391/mcp`，请求须带 `Authorization: Bearer <MCP_TOKEN>`。它绝不监听局域网或公网地址。

可选地用 `MCP_ALLOWED_ROOTS` 限制 Agent 只能管理特定工作区；多个路径以操作系统的路径分隔符连接（macOS/Linux 使用 `:`，Windows 使用 `;`）：

```bash
MCP_ALLOWED_ROOTS='/Users/me/robots:/Users/me/workspaces' albs mcp
```

一旦配置，项目读写、任务操作与新项目创建都会在服务端验证目录是否位于这些根路径内。

## 能力模型

| 层级 | MCP 能力 | 用途 |
| --- | --- | --- |
| 上下文 | `resources/list`、`resources/read` | 获取 `alemonjs://mcp/capabilities`，先了解可用范围与边界。 |
| 只读 | 项目状态、文件列表、读取源码、本地包列表 | 让 Agent 先检查，再决定是否需要修改。 |
| 修改 | 写入项目文件、创建项目 | 每次调用都必须带 `confirm: true`。 |
| 异步操作 | 启动项目操作、查询任务、列出任务 | 安装、构建、Git 操作不会阻塞 MCP 连接。 |

所有工具同时提供文本结果和 `structuredContent`，因此客户端既可向模型展示结果，也可稳定读取字段。

## 推荐的 Agent 工作流

1. 读取 `alemonjs://mcp/capabilities`。
2. 用 `alemonjs_project_status`、`alemonjs_list_project_files` 了解目标项目。
3. 读取必要的源码文件，提出修改和影响说明。
4. 得到用户确认后，以 `confirm: true` 写入文件或调用 `alemonjs_start_project_action`。
5. 对长操作使用 `alemonjs_get_project_task` 轮询到 `completed` 或 `failed`。

## 权限边界

项目根目录必须包含 `package.json`。MCP 允许管理项目源码与普通配置，但永久拒绝：

- 任意宿主机 Shell 命令、外部发布操作和长期运行的开发服务；
- `.env`、`.npmrc`、私钥/证书文件；
- `.git`、`node_modules`、符号链接；
- 超过 1 MiB 的文件读取或写入。

这些约束位于 Go 服务层，而不是依赖客户端提示。未来如需接入远程 MCP，应将 HTTP adapter 放在带 OAuth、项目范围策略、审计日志和速率限制的网关之后；不能直接把当前本机控制器暴露到公网。

协议实现遵循 [MCP 2025-06-18 工具规范](https://modelcontextprotocol.io/specification/2025-06-18/server/tools)：声明工具能力、工具元数据和结构化结果，并保持用户对修改性操作的确认权。
