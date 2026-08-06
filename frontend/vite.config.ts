import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react-swc'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api': 'http://localhost:17390'
    }
  },
  build: {
    outDir: '../dist',
    emptyOutDir: true,
    rolldownOptions: {
      output: {
        manualChunks(id) {
          if (
            id.includes('node_modules/react/') ||
            id.includes('node_modules/react-dom/') ||
            id.includes('node_modules/react-router-dom/')
          )
            return 'react'
          if (
            id.includes('node_modules/@reduxjs/') ||
            id.includes('node_modules/react-redux/') ||
            id.includes('node_modules/redux-persist/')
          )
            return 'state'
          if (id.includes('node_modules/markdown-to-jsx/')) return 'markdown'
        }
      }
    }
  }
})
