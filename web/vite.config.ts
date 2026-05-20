import path from 'node:path';
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// Continuum mounts each plugin under /api/v1/plugins/{installationId}/.
// The plugin doesn't know the mount path at build time, so we use a
// relative base so asset URLs resolve against the current page.
// outDir matches the Go //go:embed path in internal/server/spa.go.
export default defineConfig({
  base: './',
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  build: {
    outDir: '../internal/server/public/dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'jsdom',
    exclude: ['node_modules/**', 'dist/**'],
  },
});
