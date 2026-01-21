import devtoolsJson from 'vite-plugin-devtools-json';
import { paraglideVitePlugin } from '@inlang/paraglide-js';
import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import Icons from 'unplugin-icons/vite';
import { codecovSvelteKitPlugin } from '@codecov/sveltekit-plugin';

export default defineConfig({
	plugins: [
		tailwindcss(),
		sveltekit(),
		paraglideVitePlugin({
			project: './project.inlang',
			outdir: './src/lib/paraglide',
			cookieName: 'locale',
			strategy: ['cookie', 'preferredLanguage', 'baseLocale']
		}),
		devtoolsJson(),
		Icons({
			compiler: 'svelte',
			autoInstall: true
		}),
		codecovSvelteKitPlugin({
			enableBundleAnalysis: true,
			bundleName: 'arcane-frontend',
			oidc: {
				useGitHubOIDC: true
			}
		})
	],
	build: {
		target: 'es2022'
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
