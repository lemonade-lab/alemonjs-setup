import {defineConfig} from 'lvyjs';
import {dirname, join} from 'path';
import {fileURLToPath} from 'url';
const __dirname = dirname(fileURLToPath(import.meta.url));
export default defineConfig({
  watch: ['src/**/*.{ts,tsx,js,jsx,json,html}'],
  alias: {
    entries: [{find: '@src', replacement: join(__dirname, 'src')}]
  },
  assets: {
    // 支持图片、字体、文本等静态资源
    filter: /\.(png|jpg|jpeg|gif|svg|webp|ico|yaml|txt|ttf|md)$/
  },
  build: {
    // JavaScript 项目不经过 TypeScript 编译：禁用 typescript 插件可避免它去
    // 读取 tsconfig.json（JS 项目没有），否则 lvy build 会报 TS18003。
    typescript: false,
    // 输出到 lib；入口默认 src 目录
    OutputOptions: {
      dir: 'lib'
    }
  }
});
