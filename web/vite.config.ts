import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import basicSsl from '@vitejs/plugin-basic-ssl';
import path from 'node:path';

// LabExtend serves HTTPS only. The dev server matches that: vite runs
// HTTPS via basic-ssl (auto self-signed) and proxies /api to the Go
// backend's HTTPS listener with TLS verification disabled (the backend's
// self-signed cert wouldn't otherwise validate from Node).
export default defineConfig({
  plugins: [react(), basicSsl()],
  resolve: {
    alias: { '@': path.resolve(__dirname, 'src') },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'https://localhost:10000',
        changeOrigin: true,
        ws: true,
        secure: false,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
});
