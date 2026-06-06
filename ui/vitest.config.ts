import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'node:path';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
    // Match vite.config.ts ordering — prefer TS source over auto-generated JS
    // shadows that some watch tools leave next to .ts/.tsx files.
    extensions: ['.mts', '.ts', '.tsx', '.mjs', '.js', '.jsx', '.json'],
  },
  test: {
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    environment: 'jsdom',
    testTimeout: 20000,
    coverage: {
      reporter: ['text', 'lcov'],
    },
  },
});
