import devtoolsJson from 'vite-plugin-devtools-json';
import { paraglideVitePlugin } from '@inlang/paraglide-js';
import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	optimizeDeps: { exclude: ['@lucide/svelte'] },
	plugins: [
		tailwindcss(),
		sveltekit(),
		paraglideVitePlugin({
			project: './project.inlang',
			outdir: './src/lib/paraglide',
			cookieName: 'locale',
			strategy: ['cookie', 'preferredLanguage', 'baseLocale']
		}),
		devtoolsJson()
	],
	build: {
		target: 'es2022',
		// Disable gzip reporting to speed up builds
		reportCompressedSize: false
	},
	server: {
		host: process.env.HOST,
		proxy: {
			'/api': {
				target: process.env.DEV_BACKEND_URL || 'http://localhost:3552',
				changeOrigin: true,
				ws: true
			}
		}
	}
});
