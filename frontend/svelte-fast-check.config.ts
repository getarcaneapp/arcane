import type { FastCheckConfig } from 'svelte-fast-check';

export default {
	rootDir: './',
	srcDir: './src',
	exclude: ['./src/lib/paraglide/**/*']
} satisfies FastCheckConfig;
