import { defineConfig } from 'vitest/config';

// The build lands in the Go server's embed folder. It isn't emptied by Vite, because it holds
// the .gitkeep that lets `go build` work without npm; scripts/clean-dist.mjs clears it first.
//
// `npm run dev` serves the pages with hot reload and forwards the API to a server running on
// 127.0.0.1:8080 (`share serve` with listen 127.0.0.1:8080 and public_url http://localhost:8080).
const server = 'http://127.0.0.1:8080';

export default defineConfig({
  build: {
    outDir: '../server/internal/webui/dist',
    emptyOutDir: false,
    // No inlined data: URIs, so the strict Content-Security-Policy needs no exceptions.
    assetsInlineLimit: 0,
    target: 'es2022',
  },
  oxc: {
    jsx: { runtime: 'automatic', importSource: 'preact' },
  },
  server: {
    proxy: {
      '/api': server,
      '/tus': server,
      '/manifest.webmanifest': server,
    },
  },
  test: {
    environment: 'node',
    include: ['test/**/*.test.ts'],
  },
});
