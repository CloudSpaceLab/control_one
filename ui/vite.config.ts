import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  base: '/console/',
  server: {
    port: 4173,
  },
  preview: {
    port: 4174,
  },
});
