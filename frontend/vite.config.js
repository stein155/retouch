import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// This frontend is built to static assets and embedded into the STLocal Go
// binary (see internal/web). In production STLocal serves both this bundle and
// the whole /api/* surface (including the /api/tunein and /api/logo proxies)
// from the same origin on :8000, so no proxy rewriting is needed at runtime.
//
// For local `vite dev`, point every /api call at a running STLocal instance.
const STLOCAL = process.env.STLOCAL_URL || 'http://localhost:8000'

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
