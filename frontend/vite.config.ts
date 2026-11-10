import { paraglideVitePlugin } from '@inlang/paraglide-js';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import Icons from 'unplugin-icons/vite';
import { defineConfig, searchForWorkspaceRoot, lazyPlugins } from 'vite-plus';

const devBackendURL = process.env['DEV_BACKEND_URL'] || 'http://localhost:3552';
const parsedDevBackendURL = new URL(devBackendURL);

function parseBooleanEnv(value: string | undefined): boolean | undefined {
	if (value == null || value === '') {
		return undefined;
	}

	switch (value.trim().toLowerCase()) {
		case '1':
		case 'true':
		case 'yes':
		case 'on':
			return true;
		case '0':
		case 'false':
		case 'no':
		case 'off':
			return false;
		default:
			return undefined;
	}
}

const explicitInsecureTLS = parseBooleanEnv(process.env['DEV_BACKEND_INSECURE_TLS']);
const useInsecureLocalTLS =
	explicitInsecureTLS ??
	(parsedDevBackendURL.protocol === 'https:' &&
		(parsedDevBackendURL.hostname === 'localhost' || parsedDevBackendURL.hostname === '127.0.0.1'));

export default defineConfig(() => ({
	plugins: lazyPlugins(() => [
		tailwindcss(),
		sveltekit({
			preprocess: vitePreprocess(),
			adapter: adapter({
				pages: process.env['BUILD_PATH'] ?? '../backend/frontend/dist',
				fallback: 'index.html',
				precompress: true
			})
		}),
		paraglideVitePlugin({
			project: './project.inlang',
			outdir: './src/lib/paraglide',
			cookieName: 'locale',
			strategy: ['cookie', 'preferredLanguage', 'baseLocale']
		}),
		Icons({
			compiler: 'svelte',
			autoInstall: true
		})
	]),
	build: {
		target: 'es2022',
		rolldownOptions: {
			checks: {
				pluginTimings: false
			},
			output: {
				codeSplitting: {
					groups: [
						{
							name: 'payload',
							test: (id: string) => id.includes('@sveltejs/kit/src/runtime/client/payload.js'),
							priority: 1
						},
						{
							name: 'preload',
							test: (id: string) => id.includes('vite/preload-helper'),
							priority: 1
						},
						{
							name: 'root',
							test: (id: string) => id.includes('/src/routes/+layout.')
						},
						{
							name: 'auth',
							test: (id: string) => id.includes('/src/routes/(auth)/')
						},
						{
							name: 'app',
							test: (id: string) => id.includes('/src/routes/(app)/+layout.')
						},
						{
							name: 'common',
							minShareCount: 10
						}
					]
				}
			}
		}
	},
	server: {
		allowedHosts: ['arcane-frontend-dev'],
		fs: {
			allow: [searchForWorkspaceRoot(process.cwd())]
		},
		host: process.env['HOST'],
		proxy: {
			'/api': {
				target: devBackendURL,
				changeOrigin: true,
				ws: true,
				secure: !useInsecureLocalTLS
			}
		}
	}
}));
