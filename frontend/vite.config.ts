/// <reference types="vitest" />
import { defineConfig } from 'vite'
import path from 'path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  // Use relative paths so the built assets work when served from Go's embed.FS
  // (e.g. file:// or a non-root path). In dev mode Vite ignores this.
  base: './',

  plugins: [
    // The React and Tailwind plugins are both required for Make, even if
    // Tailwind is not being actively used – do not remove them
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      // Alias @ to the src directory
      '@': path.resolve(__dirname, './src'),
    },
  },

  // File types to support raw imports. Never add .css, .tsx, or .ts files to this.
  assetsInclude: ['**/*.svg', '**/*.csv'],

  // Proxy /api to the Go backend during local development (vite dev server)
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },

  build: {
    // Single directory output for easy Go embed
    outDir: 'dist',
    assetsDir: 'assets',
  },

  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
    exclude: ['node_modules', 'dist', 'tests-e2e/**'],
    // Headroom over the 5s/10s defaults for the heaviest wizard/export tests,
    // which render the whole component tree: ~1s in the CI image standalone,
    // but the shared runner executes 7 jobs concurrently and is measurably
    // slower than that. Deliberately not larger -- a genuine hang should still
    // surface in seconds, not a minute.
    testTimeout: 20_000,
    hookTimeout: 20_000,
  },
})