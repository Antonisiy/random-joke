import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/random-joke': 'http://localhost:8888',
      '/translate': 'http://localhost:8888'
    }
  }
})
