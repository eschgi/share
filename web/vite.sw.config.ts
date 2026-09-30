import { defineConfig } from 'vite';

// The service worker is a build of its own: one classic script at /sw.js, next to the page,
// so its scope is the whole site. `npm run build` runs it after the page build.
export default defineConfig({
  publicDir: false,
  build: {
    outDir: '../server/internal/webui/dist',
    emptyOutDir: false,
    target: 'es2022',
    lib: {
      entry: 'src/sw.ts',
      formats: ['iife'],
      name: 'shareServiceWorker',
      fileName: () => 'sw.js',
    },
  },
});
