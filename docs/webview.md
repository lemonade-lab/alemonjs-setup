# ALemonX WebView 规范

Setup 会把当前机器人的插件页面嵌入后台。WebView 是插件自己的前端，不是 Setup 的管理页面，也不会获得系统命令、文件系统或 Setup 登录态。

## 注册入口

插件的 `package.json` 同时需要声明页面目录和至少一个侧栏入口：

```json
{
  "name": "example-plugin",
  "alemonjs": {
    "web": { "root": "dist" },
    "desktop": { "sidebars": [{ "name": "示例插件" }] }
  }
}
```

`root` 必须是包目录中的相对路径，并且必须有 `index.html`。Setup 只扫描机器人本地 `packages` 中的包，以及机器人 `package.json` 明确声明的依赖。

## 构建要求

- 输出完整静态站点到 `dist`。
- 推荐使用相对资源路径，例如 `assets/index.js`，不要依赖站点根目录。
- Setup 已兼容 Vite 常见的 `/assets`、`favicon.ico` 与 CSS `url(/assets/...)`，但动态拼接的根绝对路径不保证可用。
- 页面只能使用自己的同源资源；外部 CDN、外部接口可能被浏览器安全策略阻止。

## 可用能力

```ts
window.__alxWebview.context // { package, name }
window.__alxWebview.postMessage(value)
window.__alxWebview.onMessage(listener)
window.__alxWebview.request('./api/example', options)
```

`request` 仅接受以 `./api/` 开头的路径，并只会代理到当前机器人应用。`postMessage` 会在首次调用时启动当前目录独立的 `alemonjs/desktop.js` desk Node.js 进程，再通过 stdin/stdout JSON IPC 与插件 desktop 模块双向通信；它不会启动机器人 `app/dev`。`appDesktopAPI.postMessage`、`appDesktopAPI.onMessage` 和 `appDesktopAPI.themeOn` 有最小兼容实现，不能视为 AlemonDesk 的完整 Wails API。

## 隔离与生命周期

- 每个机器人使用独立的 loopback `*.localhost` 源，不共享插件存储、Cookie 或 Setup 后台状态。
- 同一机器人内切换页签会保留 iframe；切换到另一机器人时，为节省资源会卸载页面，返回时重新载入。
- 插件需要自行使用 localStorage、服务端状态或其他持久化方案恢复页面数据。
- 打开页面本身不会启动机器人。机器人 HTTP API 只是页面的可选数据源；未启动时，页面应自行展示连接不可用状态。WebView 与插件后端的进程通信由独立的 desk 运行时负责。
- 插件不能访问 Setup 的文件、终端、PM2、系统命令或其他机器人目录。
