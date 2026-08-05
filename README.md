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


## 本地开发

前置要求：Go 1.21+、Node.js 22+、Yarn 1.x。

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
