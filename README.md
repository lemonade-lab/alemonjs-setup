# AlemonJS Setup

AlemonJS Setup 是面向新手的 AlemonJS 本地引导工具。它用浏览器界面完成环境检查、项目创建、应用安装和机器人管理，尽量不要求用户记住命令。

## 主要功能

- 逐步创建 AlemonJS 项目：项目名称、JavaScript / TypeScript、Git、Yarn、图片能力和开发技能均可选择。
- 自动检查 Node.js、Git、Docker 等环境，并给出中文解决建议。
- 下载 AlemonDesk 桌面版、AlemonApp 手机版，或引导部署 AlemonGo Web 版。
- 后台中心：管理当前目录或指定目录的机器人，编辑 `.npmrc`、`alemon.config.yaml`、`README.md`，重装依赖、开发启动、构建、提交代码和 PM2 后台启动。
- 插件与连接管理：界面化安装支持的 AlemonJS 能力包及 OneBot、QQ Bot、Discord 连接包。

## 下载与运行

在项目 [Releases](../../releases) 页面下载对应系统的文件：

| 系统 | 文件 |
| --- | --- |
| Windows | `alemonjs-setup-windows-amd64.exe` |
| macOS Apple Silicon | `alemonjs-setup-darwin-arm64` |
| macOS Intel | `alemonjs-setup-darwin-amd64` |
| Linux | `alemonjs-setup-linux-amd64` |

运行后访问终端中显示的本地地址，默认是 `http://localhost:17390`。

```bash
./alemonjs-setup-darwin-arm64
```

Windows 可直接双击 `.exe`。

## 本地开发

需要 Go 1.21+、Node.js 22+ 与 Yarn。

```bash
# 终端一：启动 API
go run .

# 终端二：启动前端
cd frontend
yarn install
yarn dev
```

前端开发地址：`http://localhost:5173`；Go 服务地址：`http://localhost:17390`。

## 构建与测试

```bash
make frontend-build
make test
make lint
make build
```

`make build` 会把前端 `dist/` 与本地 `templates/` 一起嵌入最终单文件，不需要用户额外下载模板。

## 自动发布

GitHub Actions 会在 main 分支推送和 Pull Request 上执行前端 lint/build、Go 测试、静态检查与编译。

推送 `v*` 标签时，会自动构建 Windows、macOS（Apple Silicon / Intel）与 Linux 单文件，并创建 GitHub Release：

```bash
git tag v0.1.0
git push origin v0.1.0
```

## 项目结构

```text
frontend/    React + Vite 界面
internal/    环境检查、项目创建与机器人管理 API
templates/   随二进制携带的 JS / TS 项目模板
.github/     GitHub Actions 流水线
```

## 安全边界

- 不覆盖已有项目目录。
- 机器人管理只接受包含 `package.json` 的本地目录。
- 插件与连接安装通过后端白名单控制，不执行任意命令。
