# ALemonX 系统插件开发与接入

系统插件用于为 ALemonX（`alx`）增加**全局的本机管理能力**，例如网络检查、系统服务或防火墙规则。它与机器人项目无关；不要用它实现某个机器人的命令、配置页或 WebView。这些能力应作为机器人插件提供。

插件的**界面就是它的 Web 界面**（`web.root` 静态前端），由 ALemonX 同源托管；系统操作通过一个**通用动作转发**接口执行。清单不再声明 `pages`/`actions`/`fields`——那些「多余配置」已移除，插件需要复杂交互时直接在自己页面里实现。发现阶段永不执行插件代码。

## 快速开始

创建如下目录：

```text
plugins/
  my-status/
    alx.json
    web/
      index.html
    runner/
      main.mjs
```

在开发仓库中，把它放到仓库根目录的 `plugins/` 下。运行中的 ALemonX 也会依次从以下位置发现插件：

1. 可执行文件同级的 `plugins/`；
2. 当前工作目录的 `plugins/`；
3. 用户配置目录的 `alx/plugins/`。

同一个插件 `id` 只会加载第一个发现的位置。插件目录的增删与 `alx.json` 修改由后台**自动热更新**（约 1 秒内反映），无需重启或手动刷新。

最小清单：

```json
{
  "id": "my-status",
  "name": "示例状态",
  "version": "1.0.0",
  "runtime": "node",
  "entry": { "darwin-arm64": "runner/main.mjs", "linux-amd64": "runner/main.mjs", "windows-amd64": "runner/main.mjs" },
  "web": { "root": "web" },
  "navigation": { "label": "示例", "icon": "circle", "order": 10 }
}
```

## `alx.json` 清单

清单文件名必须精确为 `alx.json`，最大 64 KiB，且不能是符号链接。`id` 必须匹配 `^[a-z][a-z0-9-]{1,63}$`。

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `id` | 是 | 稳定的插件标识，建议发布后不再变更。 |
| `name`、`version` | 是 | 管理台显示的名称与版本。 |
| `description` | 否 | 插件说明。 |
| `platforms` | 否 | 支持的 Go 平台名；未填写表示全部平台。 |
| `navigation` | 否 | 全局功能栏入口；`label` 默认取 `name`，`icon` 默认 `◈`，`order` 越小越靠前。 |
| `runtime` | 否 | `binary`、`node` 或 `go`；省略时为 `binary`。 |
| `entry` | 是* | 执行器映射。键为 `GOOS-GOARCH`（例如 `linux-amd64`），也可仅用 `GOOS` 作为回退。 |
| `development` | 否 | 开发回退执行器，结构与 `runtime`/`entry` 相同。 |
| `web` | **是** | 插件的 Web 界面目录（如 `web`），不能是绝对路径或含 `..`。无 `web` 的插件不可用。 |

`entry` 路径必须是插件目录内的普通文件，不能使用绝对路径、`..` 越界路径或符号链接。`node` 会以 `node <entry>` 启动；`binary` 直接执行该文件；`go` 只读取 `entry.go`，并以 `go run <entry.go>` 启动。`Runnable = web 存在且 (entry 或 development 存在)`。

## 静态 Web 界面（必选）

插件详情页会直接嵌入 `web.root/index.html`（同源 iframe）。界面用 `fetch` 调用本插件的**动作转发接口**：

```http
POST /api/v1/setup/plugins/<插件id>/actions
Content-Type: application/json

{ "action": "network-check", "confirm": false, "params": {} }
```

返回任务信息后，轮询 `/api/v1/robot/tasks` 获取结果（`status`：`running`/`completed`/`failed`，结果在 `output`，失败原因在 `error`）。

- **`action` 名称由执行器自行白名单**：alx 不校验清单，直接把请求转发给执行器，执行器内部 `switch` 只处理自己支持的动作并校验每个参数。
- **危险操作的二次确认由 Web 界面负责**（例如自绘确认弹窗）。安装插件即等于授予其执行系统命令的能力（「安装即信任」），因此不再有清单级 confirm 声明。
- 结果文本若每行以 `✓ `、`! ` 或 `? ` 开头（符号后一个空格），可按状态着色；缩进行按普通文本显示。
- 完整可参考的界面见 [alemonx-network/web](../plugins/alemonx-network/web/)（原生 HTML/JS，无构建链，含确认弹窗与结果渲染）。

安全边界：Web 界面**只对已安装且启用的插件**提供；在线识别或未安装的插件返回 404。托管仅 serve `web.root` 内的普通文件，拒绝路径穿越与符号链接逃逸，并设置 `X-Content-Type-Options` 与 CSP。

## 执行器协议

每次操作均会启动一个独立进程。工作目录为插件目录；ALemonX 会向标准输入写入一个 JSON 对象，并从标准输出读取**唯一的 JSON 响应**。不要在标准输出打印日志或调试文本；请输出到标准错误。

请求：

```json
{
  "protocol": "alx/v1",
  "method": "run",
  "action": "network-check",
  "params": {}
}
```

响应：

```json
{ "output": "✓ 已检查 3 个网卡。" }
```

操作失败时仍应正常输出 JSON，并设置 `error`：

```json
{ "output": "已检查现有规则。", "error": "需要管理员权限" }
```

进程非零退出、没有有效 JSON 响应或 `error` 非空都会将该任务标记为失败。成功但未提供 `output` 时，显示“插件操作已完成。”

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
  process.stdout.write(JSON.stringify({ output: '✓ 检查完成。' }))
} catch (error) {
  process.stdout.write(JSON.stringify({ error: String(error.message || error) }))
}
```

## 安全与发布清单

- 将每个操作实现为固定的动作分支；绝不把字段值拼接为 shell 字符串或执行用户提供的命令。
- 危险操作在执行器内校验输入与运行环境；Web 界面提供二次确认。
- 以最小权限运行；需要提权时明确提示用户，处理取消授权的情况。
- 不读取、上传或在输出中回显凭据、私钥和令牌。
- 为每个发布平台/架构提供对应的二进制，并在干净环境中测试缺少运行时、取消提权和无效输入等失败路径。
- 确保可执行器有执行权限；Windows 文件应以 `.exe` 结尾。
- **不要托管你不信任来源的插件**——安装即信任其执行系统命令与同源 Web 界面。

在本仓库开发时，至少运行：

```bash
go test ./internal/setupplugin/...
go vet ./internal/setupplugin/...
```

插件在 UI、CLI（`alx plugin list`）与 MCP（`alemonjs_list_setup_plugins`，列出插件及其 Web 入口）中均可发现。详见 [MCP 控制面文档](mcp.md)。
