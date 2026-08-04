import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react-swc'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:17390',
    },
  },
  build: {
    outDir: '../dist',
    emptyOutDir: true,
  },
})
