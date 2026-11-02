import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

// The web app talks to the Go service over a configurable base URL. In dev the
// default is the local service on :8080 (see src/lib/env.ts); set
// VITE_API_BASE_URL to point anywhere else.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src')
    }
  },
  server: {
    port: 5173
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
    rollupOptions: {
      output: {
        // Split the (large) vendor/react chunk so app code re-caches.
        manualChunks: {
          react: ['react', 'react-dom']
        }
      }
    }
  }
});
