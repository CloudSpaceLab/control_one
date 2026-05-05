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
    proxy: {
      '/api': {
        target: 'https://localhost:8443',
        changeOrigin: false,
        secure: false, // self-signed CP cert in dev
        configure: (proxy) => {
          // Force the upstream Host header so the CP's deriveControlPlaneURL
          // (controlplane/internal/server/agent_download.go:828) returns a
          // URL the remote agent host can actually reach via the reverse
          // SSH tunnel (remote:18443 → local:8443). The X-Forwarded-Proto
          // hint nudges deriveControlPlaneURL toward https since the CP
          // request itself arrives over TLS.
          proxy.on('proxyReq', (proxyReq) => {
            proxyReq.setHeader('Host', '127.0.0.1:18443');
            proxyReq.setHeader('X-Forwarded-Proto', 'https');
          });
        },
      },
      '/healthz': {
        target: 'https://localhost:8443',
        changeOrigin: false,
        secure: false,
      },
    },
  },
  preview: {
    port: 4174,
  },
});
