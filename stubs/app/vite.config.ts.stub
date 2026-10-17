import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],

  // The build output lives inside public/, which Vite would otherwise treat as
  // its static-copy source — producing a warning and copying the build into
  // itself. Nothing here relies on Vite's public-directory feature: the Go side
  // embeds public/build directly, and in development the dev server serves it.
  publicDir: false,

  resolve: {
    alias: {
      // '@/Components/Foo.vue' resolves to resources/js/Components/Foo.vue.
      // tsconfig.json declares the same alias so the editor agrees with Vite.
      '@': fileURLToPath(new URL('./resources/js', import.meta.url)),
    },
  },

  // Assets are served from /build/ by the Go application, so Vite has to be told
  // the same base. Without it Vite resolves the chunks it fetches at runtime
  // (/assets/Index-xxxx.js) against the site root, where nothing is mounted: the
  // entry script loads, the page component 404s, and the page renders empty.
  // Keep this in step with ViteConfig.BuildDir on the Go side.
  base: '/build/',

  build: {
    // The manifest is how the Go side finds the built, content-hashed files
    // (VIEW-03). Without it the renderer cannot emit asset tags.
    manifest: true,
    outDir: 'public/build',
    emptyOutDir: true,
    rollupOptions: {
      input: 'resources/js/app.ts',
    },
    // Hashing is what makes the long cache lifetime safe: a changed file gets a
    // new name rather than a stale cache entry.
    assetsInlineLimit: 4096,
  },

  server: {
    // strictPort matters: `vuepher dev` reads the port from vuepher.json, and a
    // silent fallback to another port would leave the Go process loading assets
    // from a server that is not running.
    port: 5173,
    strictPort: true,
    // Expose the dev server on the same host the Go process serves from.
    origin: 'http://localhost:5173',
  },
})
