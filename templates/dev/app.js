import { start } from 'alemonjs';
// --jsxp 启动 jsxp 服务。jsxp 仅在需要图片渲染时安装，因此只在显式传入
// --jsxp 时才加载它，避免普通开发启动因缺少可选依赖而失败。
if (process.argv.includes('--jsxp')) {
  const { createServer } = await import('jsxp');
  void createServer();
} else {
  start('src/index.js');
}
