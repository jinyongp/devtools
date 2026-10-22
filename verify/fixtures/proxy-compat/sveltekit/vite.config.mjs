import { defineConfig } from 'vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { serverOptions } from '../shared/server-options.mjs';
export default defineConfig({ cacheDir: '.vite', plugins: [sveltekit()], server: serverOptions });
