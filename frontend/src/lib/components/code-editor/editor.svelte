<script lang="ts">
	import CodeMirror from 'svelte-codemirror-editor';
	import { autocompletion, type Completion, type CompletionContext } from '@codemirror/autocomplete';
	import { yaml } from '@codemirror/lang-yaml';
	import { StreamLanguage } from '@codemirror/language';
	import { properties } from '@codemirror/legacy-modes/mode/properties';
	import { linter, lintGutter } from '@codemirror/lint';
	import Ajv, { type ErrorObject, type ValidateFunction } from 'ajv';
	import { LineCounter, parseDocument } from 'yaml';
	import { arcaneDarkInit } from './theme';
	import type { Extension } from '@codemirror/state';
	import type { Diagnostic, LintSource } from '@codemirror/lint';
	import configStore from '$lib/stores/config-store';

	type CodeLanguage = 'yaml' | 'env';
	type SchemaObject = Record<string, unknown>;
	type YamlDocLike = {
		getIn: (path: Array<string | number>, keepScalar?: boolean) => unknown;
	};
	type ComposeSchemaContext = {
		completionOptions: Completion[];
		validate: ValidateFunction<unknown>;
	};

	const DOCKER_COMPOSE_SCHEMA_URL =
		'https://raw.githubusercontent.com/compose-spec/compose-go/refs/heads/main/schema/compose-spec.json';
	const MAX_SCHEMA_DIAGNOSTICS = 30;

	let composeSchemaPromise: Promise<ComposeSchemaContext | null> | null = null;

	let {
		value = $bindable(''),
		language = 'yaml' as CodeLanguage,
		placeholder = '',
		readOnly = false,
		fontSize = '12px',
		autoHeight = false,
		hasErrors = $bindable(false)
	} = $props();

	function escapeRegExp(value: string): string {
		return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
	}

	function decodePointerSegment(segment: string): string {
		return segment.replace(/~1/g, '/').replace(/~0/g, '~');
	}

	function pointerToPath(pointer: string): Array<string | number> {
		if (!pointer) return [];
		return pointer
			.split('/')
			.slice(1)
			.filter((segment) => segment.length > 0)
			.map((segment) => {
				const decoded = decodePointerSegment(segment);
				return /^\d+$/.test(decoded) ? Number(decoded) : decoded;
			});
	}

	function getObjectValue(source: unknown, key: string): unknown {
		if (!source || typeof source !== 'object') return undefined;
		return (source as SchemaObject)[key];
	}

	function collectSchemaKeysFromNode(node: unknown, keys: Set<string>, visited: WeakSet<object>) {
		if (!node || typeof node !== 'object') return;
		if (visited.has(node)) return;
		visited.add(node);

		const properties = getObjectValue(node, 'properties');
		if (properties && typeof properties === 'object' && !Array.isArray(properties)) {
			for (const [key, value] of Object.entries(properties as SchemaObject)) {
				keys.add(key);
				collectSchemaKeysFromNode(value, keys, visited);
			}
		}

		const definitions = getObjectValue(node, 'definitions');
		if (definitions && typeof definitions === 'object' && !Array.isArray(definitions)) {
			for (const definition of Object.values(definitions as SchemaObject)) {
				collectSchemaKeysFromNode(definition, keys, visited);
			}
		}

		const defs = getObjectValue(node, '$defs');
		if (defs && typeof defs === 'object' && !Array.isArray(defs)) {
			for (const definition of Object.values(defs as SchemaObject)) {
				collectSchemaKeysFromNode(definition, keys, visited);
			}
		}

		const arrayContainers = ['allOf', 'anyOf', 'oneOf', 'prefixItems'];
		for (const container of arrayContainers) {
			const value = getObjectValue(node, container);
			if (Array.isArray(value)) {
				for (const item of value) {
					collectSchemaKeysFromNode(item, keys, visited);
				}
			}
		}

		const singularContainers = ['items', 'if', 'then', 'else', 'not', 'additionalProperties'];
		for (const container of singularContainers) {
			const value = getObjectValue(node, container);
			collectSchemaKeysFromNode(value, keys, visited);
		}
	}

	function toCompletionOptions(keys: string[]): Completion[] {
		return keys
			.filter((key) => key.length > 0)
			.sort((a, b) => a.localeCompare(b))
			.map((key) => ({
				label: key,
				type: 'property',
				apply: key.endsWith(':') ? key : `${key}: `
			}));
	}

	async function getComposeSchemaContext(): Promise<ComposeSchemaContext | null> {
		if (composeSchemaPromise) return composeSchemaPromise;

		composeSchemaPromise = fetch(DOCKER_COMPOSE_SCHEMA_URL)
			.then((response) => response.json())
			.then((schema: unknown) => {
				const schemaKeys = new Set<string>();
				collectSchemaKeysFromNode(schema, schemaKeys, new WeakSet<object>());

				const ajv = new Ajv({
					allErrors: true,
					strict: false,
					strictSchema: false,
					allowUnionTypes: true,
					validateFormats: false,
					validateSchema: false
				});

				const validate = ajv.compile(schema as object);

				return {
					completionOptions: toCompletionOptions(Array.from(schemaKeys)),
					validate
				};
			})
			.catch((error) => {
				console.error('Failed to load Docker Compose schema for CodeMirror', error);
				return null;
			});

		return composeSchemaPromise;
	}

	function getNodeRangeByPath(doc: YamlDocLike, path: Array<string | number>): { from: number; to: number } | null {
		const node = doc.getIn(path, true) as { range?: [number, number, number] } | null;
		const range = node?.range;
		if (!range || range.length < 2) return null;
		return {
			from: range[0],
			to: Math.max(range[0] + 1, range[1])
		};
	}

	function findKeyRangeInSource(source: string, key: string): { from: number; to: number } | null {
		const keyRegex = new RegExp(`^\\s*${escapeRegExp(key)}\\s*:`, 'm');
		const match = keyRegex.exec(source);
		if (!match || match.index < 0) return null;
		return {
			from: match.index,
			to: Math.min(source.length, match.index + Math.max(1, key.length))
		};
	}

	function toSchemaDiagnostic(error: ErrorObject, doc: YamlDocLike, source: string): Diagnostic {
		const path = pointerToPath(error.instancePath || '');
		const params = error.params as Record<string, unknown>;
		const missingProperty = typeof params.missingProperty === 'string' ? params.missingProperty : null;
		const additionalProperty = typeof params.additionalProperty === 'string' ? params.additionalProperty : null;

		const range = getNodeRangeByPath(doc, path) ||
			(missingProperty ? findKeyRangeInSource(source, missingProperty) : null) ||
			(additionalProperty ? findKeyRangeInSource(source, additionalProperty) : null) || {
				from: 0,
				to: Math.min(source.length, 1)
			};

		let message = `${error.instancePath || '/'} ${error.message || 'is invalid'}`;
		if (error.keyword === 'required' && missingProperty) {
			message = `Missing required property "${missingProperty}"`;
		}
		if (error.keyword === 'additionalProperties' && additionalProperty) {
			message = `Unsupported property "${additionalProperty}"`;
		}

		return {
			from: range.from,
			to: range.to,
			severity: 'error',
			message
		};
	}

	function getComposeSemanticDiagnostics(parsedValue: unknown, doc: YamlDocLike): Diagnostic[] {
		if (!parsedValue || typeof parsedValue !== 'object' || Array.isArray(parsedValue)) return [];

		const services = getObjectValue(parsedValue, 'services');
		if (!services || typeof services !== 'object' || Array.isArray(services)) return [];

		const diagnostics: Diagnostic[] = [];

		for (const [serviceName, serviceValue] of Object.entries(services as SchemaObject)) {
			if (!serviceValue || typeof serviceValue !== 'object' || Array.isArray(serviceValue)) continue;

			const volumes = getObjectValue(serviceValue, 'volumes');
			if (volumes && typeof volumes === 'object' && !Array.isArray(volumes)) {
				const range = getNodeRangeByPath(doc, ['services', serviceName, 'volumes']) || { from: 0, to: 1 };
				diagnostics.push({
					from: range.from,
					to: range.to,
					severity: 'error',
					message: `Service "${serviceName}" volumes must be a list. Prefix each item with '-'.`
				});
			}
		}

		return diagnostics;
	}

	const composeCompletionSource = async (context: CompletionContext) => {
		if (language !== 'yaml' || readOnly) return null;

		const before = context.matchBefore(/[\w-]*/);
		if (!context.explicit && (!before || before.from === before.to)) return null;

		const schemaContext = await getComposeSchemaContext();
		const options = schemaContext?.completionOptions ?? [];
		if (!options.length) return null;

		return {
			from: before ? before.from : context.pos,
			options,
			validFor: /[\w-]*/
		};
	};

	function getLanguageExtension(lang: CodeLanguage): Extension[] {
		const extensions: Extension[] = [];

		switch (lang) {
			case 'yaml':
				extensions.push(yaml());
				if (!readOnly) {
					extensions.push(
						lintGutter(),
						linter(yamlLinter, { delay: 120 }),
						autocompletion({
							activateOnTyping: true,
							override: [composeCompletionSource]
						})
					);
				}
				break;
			case 'env':
				extensions.push(StreamLanguage.define(properties));
				break;
		}

		return extensions;
	}

	const yamlLinter: LintSource = async (view): Promise<Diagnostic[]> => {
		const diagnostics: Diagnostic[] = [];
		const source = view.state.doc.toString();
		const lineCounter = new LineCounter();
		const tabIndentRegex = /(^|\n)(\t+)/g;

		for (const match of source.matchAll(tabIndentRegex)) {
			const tabs = match[2] || '';
			const newlineLength = match[1] === '\n' ? 1 : 0;
			const start = (match.index ?? 0) + newlineLength;
			diagnostics.push({
				from: start,
				to: Math.max(start + 1, start + tabs.length),
				severity: 'error',
				message: 'Tabs are not allowed for YAML indentation. Use spaces only.'
			});
		}

		const parsedDocument = parseDocument(source, {
			lineCounter,
			strict: true,
			uniqueKeys: false
		});

		for (const error of parsedDocument.errors) {
			const positions = (error as { pos?: [number, number] }).pos;
			const start = positions?.[0] ?? 0;
			const end = positions?.[1] ?? Math.min(source.length, start + 1);
			diagnostics.push({
				from: start,
				to: Math.max(start + 1, end),
				severity: 'error',
				message: error.message || 'YAML syntax error'
			});
		}

		if (diagnostics.length > 0) {
			hasErrors = true;
			return diagnostics;
		}

		try {
			const parsedValue = parsedDocument.toJS();
			const schemaContext = await getComposeSchemaContext();

			if (schemaContext) {
				const valid = schemaContext.validate(parsedValue);
				if (!valid) {
					for (const error of (schemaContext.validate.errors || []).slice(0, MAX_SCHEMA_DIAGNOSTICS)) {
						diagnostics.push(toSchemaDiagnostic(error, parsedDocument, source));
					}
				}
			}

			diagnostics.push(...getComposeSemanticDiagnostics(parsedValue, parsedDocument));
		} catch {
			diagnostics.push({
				from: 0,
				to: 1,
				severity: 'error',
				message: 'YAML parsing failed'
			});
		}

		hasErrors = diagnostics.some((diagnostic) => diagnostic.severity === 'error');
		return diagnostics;
	};

	$effect(() => {
		if (language !== 'yaml' || readOnly) {
			hasErrors = false;
		}
	});

	const theme = $derived.by(() => {
		$configStore;
		return arcaneDarkInit();
	});

	const extensions = $derived([...getLanguageExtension(language), theme]);

	const styles = $derived({
		'&': {
			fontSize,
			height: autoHeight ? 'auto' : '100%'
		},
		'.cm-scroller': {
			overflow: autoHeight ? 'visible' : 'auto',
			maxHeight: autoHeight ? 'none' : '100%'
		},
		'&.cm-editor[contenteditable=false]': {
			cursor: 'not-allowed'
		},
		'.cm-content[contenteditable=false]': {
			cursor: 'not-allowed'
		}
	});
</script>

<div class="arcane-code-editor {autoHeight ? 'auto-height' : 'full-height'}">
	<CodeMirror bind:value {extensions} {styles} {placeholder} readonly={readOnly} nodebounce={true} />
</div>

<style>
	:global(.arcane-code-editor.full-height) {
		height: 100%;
	}
	:global(.arcane-code-editor.full-height .codemirror-wrapper) {
		height: 100%;
	}
	:global(.arcane-code-editor.full-height .cm-editor) {
		height: 100%;
	}
	:global(.arcane-code-editor.auto-height .codemirror-wrapper) {
		height: auto;
	}
	:global(.arcane-code-editor.auto-height .cm-editor) {
		height: auto;
		min-height: 120px;
	}
	:global(.arcane-code-editor.auto-height .cm-editor .cm-scroller) {
		overflow-y: visible;
	}
	:global(.arcane-code-editor .cm-editor .cm-scroller) {
		overflow-x: auto;
	}
	:global(.arcane-code-editor .cm-editor .cm-gutters) {
		background-color: #18181b;
		border-right: none;
	}
	:global(.arcane-code-editor .cm-editor .cm-activeLineGutter) {
		background-color: #2c313a;
		color: #e5e7eb;
	}
</style>
