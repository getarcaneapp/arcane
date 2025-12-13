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

// Import themes
import draculaTheme from './dracula-theme.json';
import lightTheme from './light-theme.json';

// Define themes
monaco.editor.defineTheme('dracula', draculaTheme as any);
monaco.editor.defineTheme('github-light', lightTheme as any);

// Register YAML language providers
registerYamlProviders(monaco);

export { monaco };
export type Monaco = typeof monaco;
