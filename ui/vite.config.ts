import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  // Served from the root of the Go binary; relative base keeps it working
  // behind a reverse-proxy sub-path too.
  base: './',
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
    target: 'es2022',
    cssCodeSplit: true,
    assetsInlineLimit: 0,
    reportCompressedSize: true
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:9100',
        changeOrigin: true
        // SSE (/api/stream) is streamed through unbuffered by the dev proxy.
      }
    }
  }
});
