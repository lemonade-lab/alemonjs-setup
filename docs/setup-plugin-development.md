# ALemonX 系统插件开发与接入

系统插件用于为 ALemonX（`alx`）增加**全局的本机管理能力**，例如网络检查、系统服务或防火墙规则。它与机器人项目无关；不要用它实现某个机器人的命令、配置页或 WebView。这些能力应作为机器人插件提供。

插件由声明式清单和可选执行器组成。ALemonX 发现、列出和渲染清单时不会执行插件代码；只有用户在界面或 MCP 客户端中明确触发某项操作后，执行器才会运行。

默认在线目录是 [Apps-X](https://github.com/lemonade-lab/alemonjs.dev/blob/main/docs/apps-x.md)。ALemonX 从其中由 `lemonade-lab` 维护的 GitHub 仓库链接读取根目录 `alx.json`，并据此渲染系统插件的入口、页签与操作。在线识别不会下载或执行远程代码；要运行操作，仍需将插件安装到本机 `plugins/` 目录。

## 快速开始

创建如下目录（目录名可以自定）：

```text
plugins/
  example-status/
    alx.json
    runner/
      main.mjs
```

在开发仓库中，把它放到仓库根目录的 `plugins/` 下。运行中的 ALemonX 也会依次从以下位置发现插件：

1. 可执行文件同级的 `plugins/`；
2. 当前工作目录的 `plugins/`；
3. 用户配置目录的 `alx/plugins/`。

同一个插件 `id` 只会加载第一个发现的位置，因此用户目录中的插件可覆盖随应用发布的同名插件。只扫描上述目录的一层子目录，隐藏目录会被忽略。刷新系统插件页面即可重新扫描，无需重启。

最小清单示例：

```json
{
  "id": "example-status",
  "name": "示例状态",
  "version": "1.0.0",
  "runtime": "node",
  "entry": { "darwin-arm64": "runner/main.mjs", "darwin-amd64": "runner/main.mjs", "linux-amd64": "runner/main.mjs", "windows-amd64": "runner/main.mjs" },
  "navigation": { "label": "示例", "icon": "circle", "order": 10 },
  "pages": [{ "id": "overview", "label": "概览" }],
  "actions": [{ "id": "check", "label": "检查状态", "page": "overview" }]
}
```

开发时可使用 Go 源码执行器，完整可运行的参考见 [网络与防火墙插件](../plugins/network-firewall/alx.json)：

```json
{
  "runtime": "binary",
  "entry": { "darwin-arm64": "dist/example-darwin-arm64" },
  "development": {
    "runtime": "go",
    "entry": { "go": "runner/main.go" }
  }
}
```

ALemonX 优先使用 `entry` 中当前系统与架构匹配的发布执行器；缺失或不可用时，才尝试 `development`。发布版本应提供已编译的 `binary` 执行器。

## `alx.json` 清单

清单文件名必须精确为 `alx.json`，最大 64 KiB，且不能是符号链接。`id`、页面 `id` 与操作 `id` 必须匹配 `^[a-z][a-z0-9-]{1,63}$`；插件、页面和操作的 ID 都不可重复。

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `id` | 是 | 稳定的插件标识，建议发布后不再变更。 |
| `name`、`version` | 是 | 管理台显示的名称与版本。 |
| `description` | 否 | 插件说明。 |
| `platforms` | 否 | 支持的 Go 平台名，如 `darwin`、`linux`、`windows`；未填写表示全部平台。 |
| `navigation` | 否 | 全局功能栏入口；`label` 默认取 `name`，`icon` 默认 `◈`，`order` 越小越靠前。 |
| `pages` | 否 | 二级页签；未填写时自动提供 `overview` 页面。 |
| `actions` | 否 | 用户可执行的操作；没有操作或没有可用执行器时，插件显示为不可运行。 |
| `runtime` | 否 | `binary`、`node` 或 `go`；省略时为 `binary`。 |
| `entry` | 运行时需要 | 发布执行器映射。键为 `GOOS-GOARCH`（例如 `linux-amd64`），也可仅用 `GOOS` 作为回退。 |
| `development` | 否 | 开发回退执行器，结构与 `runtime` / `entry` 相同。 |

`entry` 路径必须是插件目录内的普通文件，不能使用绝对路径、`..` 越界路径或符号链接。`node` 会以 `node <entry>` 启动；`binary` 直接执行该文件；`go` 只读取 `entry.go`，并以 `go run <entry.go>` 启动。

### 页面、操作与字段

页面对象包含 `id`、`label` 和可选的 `description`。操作对象包含：

```json
{
  "id": "open-port",
  "label": "开放端口",
  "description": "允许指定端口的入站连接。",
  "page": "firewall",
  "confirm": true,
  "fields": [
    {
      "key": "port",
      "label": "端口",
      "type": "number",
      "default": "17117"
    }
  ]
}
```

- `page` 用于把操作显示在指定页签；省略时由管理台默认页呈现。
- `confirm: true` 会要求用户在管理台再次确认；务必用于会修改系统、网络、服务或数据的操作。
- 每个字段有 `key`、`label`、`type`、可选的字符串 `default` 和 `options`。`type: "select"` 时，`options` 为 `{ "label": "显示名", "value": "传入值" }` 数组；其他 `type` 直接作为 HTML `<input type>` 使用，例如 `text`、`number`、`password`。
- 所有字段值最终都会作为字符串传给执行器。执行器必须自行校验必填性、格式、范围、权限与平台条件，不能信任前端输入。

## 执行器协议

每次操作均会启动一个独立进程。工作目录为插件目录；ALemonX 会向标准输入写入一个 JSON 对象，并从标准输出读取**唯一的 JSON 响应**。不要在标准输出打印日志或调试文本；请输出到标准错误，或把用户可见信息放在 `output`。

请求：

```json
{
  "protocol": "alx/v1",
  "method": "run",
  "action": "open-port",
  "params": { "port": "17117", "protocol": "tcp" }
}
```

响应：

```json
{ "output": "已开放 17117/tcp 入站端口规则。" }
```

操作失败时仍应正常输出 JSON，并设置 `error`；可同时返回部分结果：

```json
{ "output": "已检查现有规则。", "error": "需要管理员权限" }
```

进程非零退出、没有有效 JSON 响应或 `error` 非空都会将该任务标记为失败。成功但未提供 `output` 时，管理台显示“插件操作已完成。”

Node.js 最小执行器：

```js
import process from 'node:process'

let input = {}
try {
  input = JSON.parse(await new Response(process.stdin).text())
  if (input.protocol !== 'alx/v1' || input.method !== 'run') {
    throw new Error('不支持的 ALX Setup 插件协议')
  }
  if (input.action !== 'check') throw new Error('未知操作')
  process.stdout.write(JSON.stringify({ output: '检查完成。' }))
} catch (error) {
  process.stdout.write(JSON.stringify({ error: String(error.message || error) }))
}
```

## 安全与发布清单

- 将每个操作实现为固定的动作分支；绝不把字段值拼接为 shell 字符串或执行用户提供的命令。
- 对危险操作声明 `confirm: true`，并在执行器内再次校验输入和运行环境。
- 以最小权限运行；需要提权时明确提示用户，处理取消授权的情况。
- 不读取、上传或在输出中回显凭据、私钥和令牌。
- 为每个发布平台/架构提供对应的二进制，并在干净环境中测试缺少运行时、取消提权和无效输入等失败路径。
- 确保可执行器有执行权限；Windows 文件应以 `.exe` 结尾。

在本仓库开发时，至少运行：

```bash
go test ./internal/setupplugin/...
go vet ./internal/setupplugin/...
```

插件在 UI、CLI（`alx plugin list`）与 MCP 中均可发现。MCP 调用危险操作同样必须传递真实的用户确认；详见 [MCP 控制面文档](mcp.md)。
