import { serverOptions } from '../shared/server-options.mjs';
export default defineNuxtConfig({
  devtools: { enabled: true },
  vite: { cacheDir: '.nuxt/compat-vite', server: serverOptions },
});
