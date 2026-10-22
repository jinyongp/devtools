import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import { serverOptions } from '../shared/server-options.mjs';
export default defineConfig({ cacheDir: '.vite', plugins: [vue()], server: serverOptions });
