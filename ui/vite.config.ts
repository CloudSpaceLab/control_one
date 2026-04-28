import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import path from 'node:path';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: '/console/',
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
    extensions: ['.mts', '.ts', '.tsx', '.mjs', '.js', '.jsx', '.json'],
  },
  build: {
    chunkSizeWarningLimit: 800,
    rollupOptions: {
      output: {
        manualChunks: {
          'react-vendor': ['react', 'react-dom', 'react-router-dom'],
          'tanstack': ['@tanstack/react-query', '@tanstack/react-table'],
          'charts': ['chart.js', 'react-chartjs-2', 'chartjs-adapter-date-fns'],
          'tremor': ['@tremor/react'],
          'radix': [
            '@radix-ui/react-dialog',
            '@radix-ui/react-dropdown-menu',
            '@radix-ui/react-popover',
            '@radix-ui/react-tabs',
            '@radix-ui/react-tooltip',
            '@radix-ui/react-toggle-group',
            '@radix-ui/react-hover-card',
            '@radix-ui/react-separator',
            '@radix-ui/react-slot',
          ],
          'editor': ['xterm', 'xterm-addon-fit'],
        },
      },
    },
  },
  server: {
    port: 4173,
  },
  preview: {
    port: 4174,
  },
});
