import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: {
    // esbuild's CSS minifier rewrites `(max-width: Npx)` media queries into
    // the CSS4 range syntax `(width<=Npx)` unless the target excludes
    // browsers that lack support for it. Safari only gained range media
    // query support in 16.4, so an unconstrained target silently breaks
    // every responsive breakpoint in the app on older Safari versions.
    target: ['es2020', 'safari15'],
  },
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://manager:8080',
        changeOrigin: true,
      },
    },
  },
});
