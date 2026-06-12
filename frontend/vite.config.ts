import { paraglideVitePlugin } from '@inlang/paraglide-js';
import adapter from '@sveltejs/adapter-static';
import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { defineConfig, searchForWorkspaceRoot } from 'vite';
import Icons from 'unplugin-icons/vite';
import packageJson from './package.json' with { type: 'json' };

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
// Allow local self-signed HTTPS while developing edge mTLS against the manager.
const useInsecureLocalTLS =
	explicitInsecureTLS ??
	(parsedDevBackendURL.protocol === 'https:' &&
		(parsedDevBackendURL.hostname === 'localhost' || parsedDevBackendURL.hostname === '127.0.0.1'));

function normalizeModuleID(id: string): string {
	return id.replaceAll('\\', '/');
}

function splitVendorChunk(id: string): string | null {
	const moduleID = normalizeModuleID(id);

	if (moduleID.includes('/src/lib/paraglide/messages/')) {
		return 'paraglide-messages';
	}

	if (moduleID.includes('/src/lib/paraglide/')) {
		return 'paraglide-runtime';
	}

	if (moduleID.includes('/node_modules/@codemirror/') || moduleID.includes('/node_modules/svelte-codemirror-editor/')) {
		return 'editor';
	}

	if (moduleID.includes('/node_modules/@xyflow/')) {
		return 'network-diagram';
	}

	if (moduleID.includes('/node_modules/@xterm/')) {
		return 'terminal';
	}

	if (moduleID.includes('/node_modules/@tanstack/')) {
		return 'query-table';
	}

	if (
		moduleID.includes('/node_modules/bits-ui/') ||
		moduleID.includes('/node_modules/formsnap/') ||
		moduleID.includes('/node_modules/paneforge/') ||
		moduleID.includes('/node_modules/vaul-svelte/')
	) {
		return 'ui-primitives';
	}

	if (moduleID.includes('/node_modules/')) {
		return 'vendor';
	}

	return null;
}

export default defineConfig({
	plugins: [
		tailwindcss(),
		sveltekit({
			preprocess: vitePreprocess(),
			adapter: adapter({
				pages: process.env['BUILD_PATH'] ?? '../backend/frontend/dist',
				fallback: 'index.html'
			}),
			version: {
				name: packageJson.version
			}
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
	],
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
							name: splitVendorChunk,
							minSize: 20 * 1024,
							maxSize: 450 * 1024
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
});
