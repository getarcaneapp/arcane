import type * as Monaco from 'monaco-editor';

/**
 * Arcane-specific extensions to the Docker Compose spec
 */
const ARCANE_CUSTOM_SCHEMA = {
	properties: {
		models: {
			type: 'object',
			description: 'Language models that will be used by your application.'
		}
	},
	definitions: {
		model: {
			type: 'object',
			description: 'Language Model for the Compose application.',
			properties: {
				name: { type: 'string', description: 'Custom name for this model.' },
				model: { type: 'string', description: 'Language Model to run.' },
				context_size: { type: 'integer' },
				runtime_flags: {
					type: 'array',
					items: { type: 'string' },
					description: 'Raw runtime flags to pass to the inference engine.'
				}
			},
			required: ['model']
		}
	}
};

/**
 * Schema Manager to handle Docker Compose specification
 */
class ComposeSchemaManager {
	private schema: any = ARCANE_CUSTOM_SCHEMA;
	private suggestionsMap: Map<string, any[]> = new Map();
	public readonly ready: Promise<void>;

	constructor() {
		this.parseSchema();
		this.ready = this.fetchLatestSchema();
	}

	private async fetchLatestSchema(retries = 2): Promise<void> {
		try {
			const response = await fetch(
				'https://raw.githubusercontent.com/compose-spec/compose-go/refs/heads/main/schema/compose-spec.json'
			);
			if (!response.ok) throw new Error(`HTTP ${response.status}`);

			const latestSchema = await response.json();
			// Merge latest official schema with our custom fields
			this.schema = {
				...latestSchema,
				properties: {
					...(latestSchema.properties || {}),
					...ARCANE_CUSTOM_SCHEMA.properties
				},
				definitions: {
					...(latestSchema.definitions || {}),
					...ARCANE_CUSTOM_SCHEMA.definitions
				}
			};
			// Clear and re-parse
			this.suggestionsMap.clear();
			this.parseSchema();
		} catch (e) {
			if (retries > 0) {
				await new Promise((r) => setTimeout(r, 1000));
				return this.fetchLatestSchema(retries - 1);
			}
			console.error('Failed to fetch Docker Compose schema after retries', e);
		}
	}

	private parseSchema() {
		// 1. Parse Root properties
		const rootSuggestions: any[] = [];
		if (this.schema.properties) {
			for (const [key, value] of Object.entries(this.schema.properties)) {
				const val = value as any;
				rootSuggestions.push({
					label: key,
					kind: 1, // Property
					documentation: this.getDescription(val),
					insertText: this.generateInsertText(key, val)
				});
			}
		}
		this.suggestionsMap.set('root', rootSuggestions);

		// 2. Parse all definitions dynamically
		if (this.schema.definitions) {
			for (const [defName, defValue] of Object.entries(this.schema.definitions)) {
				const def = defValue as any;
				if (def.properties) {
					const suggestions: any[] = [];
					for (const [key, value] of Object.entries(def.properties)) {
						const val = value as any;
						suggestions.push({
							label: key,
							kind: 1, // Property
							documentation: this.getDescription(val),
							insertText: this.generateInsertText(key, val)
						});
					}
					this.suggestionsMap.set(defName, suggestions);
				}
			}
		}
	}

	private getDescription(val: any, visited = new Set<string>()): string {
		if (!val) return '';
		if (val.description) return val.description;

		// Resolve $ref
		if (val.$ref) {
			if (visited.has(val.$ref)) return ''; // Circular reference
			visited.add(val.$ref);
			// Handle #/definitions/name format and simple #name format
			const defName = val.$ref.includes('/') ? val.$ref.split('/').pop() : val.$ref.replace('#', '');
			const def = this.schema.definitions?.[defName];
			if (def && def.description) return def.description;
			if (def) return this.getDescription(def, visited);
		}

		// Resolve oneOf/anyOf
		const nested = val.oneOf || val.anyOf || [];
		for (const sub of nested) {
			const desc = this.getDescription(sub, visited);
			if (desc) return desc;
		}

		return '';
	}

	private generateInsertText(key: string, val: any): string {
		// Handle array of types or single type
		const type = Array.isArray(val.type) ? val.type[0] : val.type;

		if (type === 'array') {
			return `${key}:\n  - \${1}`;
		}
		// If it has properties, a $ref, or is explicitly an object, treat as nested
		if (type === 'object' || val.$ref || val.properties || val.oneOf || val.anyOf) {
			return `${key}:\n  \${1}`;
		}
		// Default for strings, numbers, booleans
		return `${key}: `;
	}

	getSuggestions(context: string) {
		const suggestions = this.suggestionsMap.get(context);
		if (suggestions && suggestions.length > 0) return suggestions;

		// Fallback: search for a property named 'context' in all definitions
		// and return its sub-properties if it has any.
		// This handles cases like 'build' which are not top-level definitions.
		if (this.schema.definitions) {
			for (const def of Object.values(this.schema.definitions) as any[]) {
				if (def.properties?.[context]) {
					const prop = def.properties[context];
					// Try to find properties directly or inside oneOf/anyOf
					const subProps =
						prop.properties ||
						prop.oneOf?.find((p: any) => p.properties)?.properties ||
						prop.anyOf?.find((p: any) => p.properties)?.properties;

					if (subProps) {
						return Object.entries(subProps).map(([key, value]) => ({
							label: key,
							kind: 1, // Property
							documentation: (value as any).description || '',
							insertText: this.generateInsertText(key, value)
						}));
					}
				}
			}
		}

		return [];
	}

	getDocumentation(word: string): string | null {
		// 1. Check root properties
		if (this.schema.properties?.[word]) {
			return `**${word}**\n\n${this.getDescription(this.schema.properties[word])}`;
		}

		// 2. Check if the word itself is a definition (e.g. "service", "network")
		if (this.schema.definitions?.[word]) {
			return `**${word}**\n\n${this.schema.definitions[word].description || ''}`;
		}

		// 3. Search through all properties of all definitions
		if (this.schema.definitions) {
			for (const [defName, def] of Object.entries(this.schema.definitions) as any[]) {
				// Check direct properties
				if (def.properties?.[word]) {
					return `**${word}** (in ${defName})\n\n${this.getDescription(def.properties[word])}`;
				}

				// Check properties inside oneOf/anyOf/allOf
				const nested = [...(def.oneOf || []), ...(def.anyOf || []), ...(def.allOf || [])];
				for (const sub of nested) {
					if (sub.properties?.[word]) {
						return `**${word}** (in ${defName})\n\n${this.getDescription(sub.properties[word])}`;
					}
				}
			}
		}
		return null;
	}
}

const schemaManager = new ComposeSchemaManager();

/**
 * Detect the current context in the YAML file
 */
function getContext(model: Monaco.editor.ITextModel, position: Monaco.Position): string {
	const lineContent = model.getLineContent(position.lineNumber);
	const indent = lineContent.match(/^\s*/)?.[0].length || 0;

	if (indent === 0) return 'root';

	let currentIndent = indent;
	let parentKey = null;
	let grandParentKey = null;

	for (let i = position.lineNumber - 1; i >= 1; i--) {
		const line = model.getLineContent(i);
		const lineIndent = line.match(/^\s*/)?.[0].length || 0;
		if (lineIndent < currentIndent) {
			const match = line.match(/^\s*([\w-]+):/);
			if (match) {
				if (parentKey === null) {
					parentKey = match[1];
					currentIndent = lineIndent;
				} else {
					grandParentKey = match[1];
					break;
				}
			}
		}
	}

	// Map plural root keys to singular definition names
	const contextMap: Record<string, string> = {
		services: 'service',
		networks: 'network',
		volumes: 'volume',
		models: 'model',
		configs: 'config',
		secrets: 'secret',
		// Common nested keys that map to definitions
		build: 'build',
		deploy: 'deployment',
		healthcheck: 'healthcheck',
		resources: 'resources',
		limits: 'limits',
		reservations: 'reservations',
		restart_policy: 'restart_policy',
		update_config: 'update_config',
		rollback_config: 'rollback_config',
		logging: 'logging'
	};

	// 1. Check if parent or grandparent is a known root key
	if (parentKey && contextMap[parentKey]) return contextMap[parentKey];
	if (grandParentKey && contextMap[grandParentKey]) return contextMap[grandParentKey];

	return 'unknown';
}

/**
 * Register YAML completion provider for Docker Compose files
 */
export function registerYamlCompletionProvider(monaco: typeof Monaco) {
	return monaco.languages.registerCompletionItemProvider('yaml', {
		triggerCharacters: [' ', ':'],
		provideCompletionItems: (model, position) => {
			const word = model.getWordUntilPosition(position);
			const range = {
				startLineNumber: position.lineNumber,
				endLineNumber: position.lineNumber,
				startColumn: word.startColumn,
				endColumn: word.endColumn
			};

			const context = getContext(model, position);
			const suggestions = schemaManager.getSuggestions(context).map((s) => ({
				...s,
				range,
				insertTextRules: s.insertText.includes('$')
					? monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet
					: undefined
			}));

			return { suggestions };
		}
	});
}

/**
 * Register YAML hover provider for documentation
 */
export function registerYamlHoverProvider(monaco: typeof Monaco) {
	return monaco.languages.registerHoverProvider('yaml', {
		provideHover: (model, position) => {
			const word = model.getWordAtPosition(position);
			if (!word) return null;

			const documentation = schemaManager.getDocumentation(word.word);
			if (documentation) {
				return {
					range: new monaco.Range(
						position.lineNumber,
						word.startColumn,
						position.lineNumber,
						word.endColumn
					),
					contents: [{ value: documentation }]
				};
			}

			return null;
		}
	});
}

/**
 * Register all YAML providers for Docker Compose editing
 */
export function registerYamlProviders(monaco: typeof Monaco) {
	return [registerYamlCompletionProvider(monaco), registerYamlHoverProvider(monaco)];
}

