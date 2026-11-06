import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import path from 'node:path';
import { consoleBaseRedirect } from './build/console-base';

export default defineConfig(({ mode }) => {
  // Proxy settings are server-only. Vite does not load .env files into
  // process.env before evaluating this configuration; load them explicitly.
  const fileEnv = loadEnv(mode, __dirname, 'CONTROL_ONE_');
  const apiTarget = process.env.CONTROL_ONE_API_URL || fileEnv.CONTROL_ONE_API_URL || 'http://127.0.0.1:8443';
  const agentPublicURL = process.env.CONTROL_ONE_AGENT_PUBLIC_URL || fileEnv.CONTROL_ONE_AGENT_PUBLIC_URL || 'https://127.0.0.1:18443';

  return {
  plugins: [consoleBaseRedirect(), react(), tailwindcss()],
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
        target: apiTarget,
        changeOrigin: false,
        secure: false, // allow either local HTTP or a self-signed HTTPS endpoint
        configure: (proxy) => {
          // Advertise an agent URL reachable by the enrolling host.
          // local.ps1 sets this to the LAN UI proxy for local demos.
          proxy.on('proxyReq', (proxyReq) => {
            const publicURL = new URL(agentPublicURL);
            proxyReq.setHeader('Host', publicURL.host);
            proxyReq.setHeader('X-Forwarded-Proto', publicURL.protocol.slice(0, -1));
          });
        },
      },
      '/healthz': {
        target: apiTarget,
        changeOrigin: false,
        secure: false,
      },
    },
  },
  preview: {
    port: 4174,
  },
  };
});
