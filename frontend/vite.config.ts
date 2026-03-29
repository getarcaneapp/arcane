import devtoolsJson from 'vite-plugin-devtools-json';
import { paraglideVitePlugin } from '@inlang/paraglide-js';
import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import Icons from 'unplugin-icons/vite';

const devBackendURL = process.env['DEV_BACKEND_URL'] || 'https://localhost:3552';
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
		})
	],
	build: {
		target: 'es2022'
	},
	server: {
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
});
