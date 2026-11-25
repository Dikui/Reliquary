import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

const shelfPath = '/api/v1/showcase/shelf'
const backendPath = /^\/(?:api|mcp|files)(?:\/|$)/

export default defineConfig({
  plugins: [
    vue(),
    {
      name: 'showcase-api',
      configureServer(server) {
        server.middlewares.use((request, response, next) => {
          const path = request.url?.split('?', 1)[0] ?? ''
          if (path === shelfPath && request.method === 'GET') return next()
          if (path === shelfPath) return response.writeHead(405, { Allow: 'GET' }).end()
          if (backendPath.test(path)) return response.writeHead(404).end()
          next()
        })
      },
    },
  ],
  server: {
    port: 5174,
    strictPort: true,
    proxy: {
      ['^' + shelfPath + '(?:\\?|$)']: { target: process.env.SHOWCASE_API_TARGET || 'http://localhost:8080' },
    },
  },
})
