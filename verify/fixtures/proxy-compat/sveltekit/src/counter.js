import { writable } from 'svelte/store';
// Svelte replaces edited component instances; module state survives that update.
export const count = writable(0);
