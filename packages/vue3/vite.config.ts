import { defineConfig } from 'vite'
import dts from 'vite-plugin-dts'

export default defineConfig({
  build: {
    lib: {
      entry: 'src/index.ts',
      name: 'Vuepher',
      formats: ['es', 'cjs'],
      fileName: (format) => (format === 'es' ? 'index.js' : 'index.cjs'),
    },
    // Vue is a peer dependency: bundling it would give the application two Vue
    // runtimes and break reactivity in confusing ways.
    rollupOptions: {
      external: ['vue'],
      output: {
        globals: { vue: 'Vue' },
      },
    },
    sourcemap: true,
    emptyOutDir: true,
  },
  plugins: [dts({ rollupTypes: true })],
})
