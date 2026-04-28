import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { resolve } from 'node:path';

export default defineConfig(({ mode }) => ({
  base: '/',
  plugins: [svelte()],
  resolve: {
    alias: {
      $lib: resolve(__dirname, 'src/lib'),
      $components: resolve(__dirname, 'src/components'),
      $stores: resolve(__dirname, 'src/stores'),
      $tabs: resolve(__dirname, 'src/tabs')
    },
    conditions: mode === 'test' ? ['browser'] : []
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsDir: 'assets',
    manifest: true,
    sourcemap: false,
    target: 'es2022',
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]'
      }
    }
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:7073', changeOrigin: true, ws: true },
      '/icons': 'http://localhost:7073',
      '/manifest.webmanifest': 'http://localhost:7073',
      '/sw.js': 'http://localhost:7073',
      '/offline.html': 'http://localhost:7073'
    }
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts']
  }
}));
