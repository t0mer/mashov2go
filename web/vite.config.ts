import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import { rmSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const outDir = resolve(dirname(fileURLToPath(import.meta.url)), '../internal/webui/dist');

// Vite's emptyOutDir would also delete the tracked .gitkeep placeholder that keeps
// the Go embed compiling before a build, so only the build output is cleared.
function clearBuildOutput(): Plugin {
  return {
    name: 'clear-build-output',
    buildStart() {
      rmSync(resolve(outDir, 'assets'), { recursive: true, force: true });
      rmSync(resolve(outDir, 'index.html'), { force: true });
    },
  };
}

// The build lands in the Go embed directory, so the binary serves the UI itself
// (guidelines/ui.md). In dev, the API is proxied to a locally running mashov2go.
export default defineConfig({
  plugins: [react(), clearBuildOutput()],
  build: {
    outDir,
    emptyOutDir: false,
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/metrics': 'http://127.0.0.1:8080',
      '/healthz': 'http://127.0.0.1:8080',
    },
  },
});
