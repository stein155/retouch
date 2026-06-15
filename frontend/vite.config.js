import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// This frontend is built to static assets and embedded into the ReTouch Go
// binary (see internal/web). In production ReTouch serves both this bundle and
// the whole /api/* surface (including the /api/tunein and /api/logo proxies)
// from the same origin, so no proxy rewriting is needed at runtime.
//
// For local `vite dev`, point every /api call at a running ReTouch instance.
const STLOCAL = process.env.STLOCAL_URL || 'http://localhost'

export default defineConfig({
  plugins: [react()],
  base: './',
  build: {
    // Build straight into the Go embed dir (internal/web/dist) so `go build`
    // always bundles the latest UI.
    outDir: '../internal/web/dist',
    emptyOutDir: true,
    assetsDir: 'assets',
  },
  server: {
    proxy: {
      '/api': { target: STLOCAL, changeOrigin: true },
    },
  },
})
