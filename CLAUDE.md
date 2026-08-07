# AlemonJS Setup — 项目约定

## 前端样式规矩

本项目前端样式体系：**Tailwind CSS + SCSS + classnames**。

写样式时遵守：

1. **优先用 Tailwind 工具类表达**，在 JSX 里直接写 `className`（配合 `classnames` 做条件拼接）。能用 Tailwind 表达的就不写自定义 CSS。
2. **不要无限增长 `styles.css` 和自定义 class**。新增 UI 时，先想能否用现有 Tailwind 类或主题变量（`--theme-*`）组合出来；只有无法用工具类表达、或需要复用复杂样式时才新增 SCSS 规则。
3. **深色主题统一走 `[data-theme='dark']` 变量映射**，不要为单个组件写硬编码颜色造成亮色孤岛。
4. 已有的 `.agent-*`、`.git-*` 等大段自定义 CSS 是历史遗留，新代码不要再沿用这种模式。
