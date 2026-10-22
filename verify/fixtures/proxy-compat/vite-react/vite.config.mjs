import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { serverOptions } from '../shared/server-options.mjs';
export default defineConfig({ cacheDir: '.vite', plugins: [react()], server: serverOptions });
