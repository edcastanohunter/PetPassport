import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// Configuración de Vite (docs/phase-1/07 §1, §9). La configuración de runtime
// (issuer, client id, API base) se lee de /config.json para usar la misma
// imagen en todos los entornos.
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: 5173,
    strictPort: true,
  },
  build: {
    sourcemap: false,
    chunkSizeWarningLimit: 600,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules')) {
            if (
              id.includes('primevue') ||
              id.includes('@primevue/themes') ||
              id.includes('primeicons')
            ) {
              return 'ui'
            }
            if (
              id.includes('vue') ||
              id.includes('pinia') ||
              id.includes('vue-router') ||
              id.includes('vue-i18n')
            ) {
              return 'vendor'
            }
          }
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.spec.ts'],
  },
})
