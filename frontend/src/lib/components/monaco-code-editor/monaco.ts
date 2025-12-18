import * as monaco from 'monaco-editor';
import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';
import jsonWorker from 'monaco-editor/esm/vs/language/json/json.worker?worker';
import cssWorker from 'monaco-editor/esm/vs/language/css/css.worker?worker';
import htmlWorker from 'monaco-editor/esm/vs/language/html/html.worker?worker';
import tsWorker from 'monaco-editor/esm/vs/language/typescript/ts.worker?worker';

// Configure Monaco to use Vite's native web worker support
self.MonacoEnvironment = {
	getWorker: function (_: any, label: string) {
		if (label === 'json') {
			return new jsonWorker();
		}
		if (label === 'css' || label === 'scss' || label === 'less') {
			return new cssWorker();
		}
		if (label === 'html' || label === 'handlebars' || label === 'razor') {
			return new htmlWorker();
		}
		if (label === 'typescript' || label === 'javascript') {
			return new tsWorker();
		}
		return new editorWorker();
	}
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

// Register YAML language providers
registerYamlProviders(monaco);

if (typeof window !== 'undefined') {
	(window as any).monaco = monaco;
}

export { monaco };
export type Monaco = typeof monaco;
