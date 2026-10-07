import { defineConfig } from 'vitest/config';

// The build lands in the Go server's embed folder. It isn't emptied by Vite, because it holds
// the .gitkeep that lets `go build` work without npm; scripts/clean-dist.mjs clears it first.
//
// `npm run dev` serves the pages with hot reload and forwards the API to a server running on
// 127.0.0.1:8080 (`share serve` with listen 127.0.0.1:8080 and public_url http://localhost:8080).
// That server must have no "proxy" in config.json: this forwarding names no visitor, so Share
// takes it as a request from its own machine. The Host header stays the page's own, so that
// Share's check of where a change comes from also passes for a phone at home on
// http://192.168.…:5173 (`npm run dev -- --host`), whose browser sends no Sec-Fetch-Site.
const server = { target: 'http://127.0.0.1:8080', changeOrigin: false };

export default defineConfig({
  build: {
    outDir: '../server/internal/webui/dist',
    emptyOutDir: false,
    // No inlined data: URIs, so the strict Content-Security-Policy needs no exceptions.
    assetsInlineLimit: 0,
    target: 'es2022',
    // Three pages: sending (screens 1–6), the invite page (9) and the setup of a new server's
    // storage folder, which need no Uppy.
    rolldownOptions: {
      input: { index: 'index.html', join: 'join.html', setup: 'setup.html' },
    },
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
    // Vitest empties CSS files, even read ?raw; the theme tests read themes.css.
    css: { include: [/themes\.css/] },
  },
});
