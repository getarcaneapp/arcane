import * as monaco from 'monaco-editor';
import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';

// Configure Monaco to use Vite's native web worker support
// Only the base editor worker is needed for YAML and INI (basic languages)
self.MonacoEnvironment = {
	getWorker: () => new editorWorker()
};

// Register basic languages for syntax highlighting
import 'monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution.js';
import 'monaco-editor/esm/vs/basic-languages/ini/ini.contribution.js';

// Import editor implementation
import 'monaco-editor/esm/vs/editor/editor.all.js';

// Import YAML providers for Docker Compose
import { registerYamlProviders } from './yaml-providers';
import { shikiToMonaco } from '@shikijs/monaco';
import { createHighlighter } from 'shiki';


/**
 * Initialize Shiki highlighter for Monaco
 */
let shikiPromise: Promise<void> | null = null;

export async function initShiki(monacoInstance: typeof monaco) {
	if (shikiPromise) return shikiPromise;

	shikiPromise = (async () => {
		const highlighter = await createHighlighter({
			themes: ['catppuccin-mocha', 'catppuccin-latte'],
			langs: ['yaml', 'ini']
		});

		// Register the languageIds first. Only registered languages will be highlighted.
		const registeredLanguages = monacoInstance.languages.getLanguages().map((lang) => lang.id);
		const langsToRegister = ['yaml', 'ini'];

		for (const lang of langsToRegister) {
			if (!registeredLanguages.includes(lang)) {
				monacoInstance.languages.register({ id: lang });
			}
		}

		// Register the themes from Shiki, and provide syntax highlighting for Monaco.
		shikiToMonaco(highlighter, monacoInstance);
	})();

	return shikiPromise;
}

// Register YAML language providers (only once, survives hot reload)
const globalWithFlag = globalThis as typeof globalThis & { _yamlProvidersRegistered?: boolean };
if (!globalWithFlag._yamlProvidersRegistered) {
	registerYamlProviders(monaco);
	globalWithFlag._yamlProvidersRegistered = true;
}

if (typeof window !== 'undefined') {
	(window as unknown as { monaco: typeof monaco }).monaco = monaco;
}

export { monaco };
export type Monaco = typeof monaco;
