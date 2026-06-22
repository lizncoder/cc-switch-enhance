import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue()],
  build: {
    // WebView2 (Chromium) ships with Windows 10/11; target a recent Chrome to
    // keep the bundle small without shipping unnecessary transpilation.
    target: 'chrome110',
    minify: 'esbuild',
    cssCodeSplit: false,
    sourcemap: false,
  },
})
