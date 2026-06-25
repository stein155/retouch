/// <reference types="vitest/config" />
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
  // Use the automatic JSX runtime everywhere (incl. Vitest's transform) so test
  // files need not import React explicitly.
  esbuild: { jsx: 'automatic' },
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
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.js',
    css: true,
    testTimeout: 15000,
  },
})
