import type { ErrorObject } from 'ajv';
import type { Diagnostic } from '@codemirror/lint';
import type { EditorView } from '@codemirror/view';
import { LineCounter, isMap, isPair, isScalar, isSeq, parseDocument, type Pair, type ParsedNode, type Scalar } from 'yaml';
import type { AnalysisResult, EditorContext, OutlineItem } from './types';
import type { ComposeSchemaContext } from './compose-schema';
import { getMissingComposeVariables, resolveVariableSource } from './vars-analysis';

const MAX_SCHEMA_DIAGNOSTICS_DEFAULT = 30;
const TAB_INDENT_REGEX = /(^|\n)(\t+)/g;
const SECRET_NAME_REGEX = /(password|passwd|secret|token|api[_-]?key|private[_-]?key|credential|aws_secret)/i;
const SECRET_VALUE_REGEX = /(?:-----BEGIN [A-Z ]+-----|eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+|[A-Za-z0-9_\/-]{24,})/;
const VARIABLE_TOKEN_REGEX = /\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(?::[-?+])[^}]*)?\}|\$([A-Za-z_][A-Za-z0-9_]*)/g;
const LIST_FIELDS = ['volumes', 'ports', 'env_file', 'dns', 'tmpfs'];

type YamlDocLike = {
	getIn: (path: Array<string | number>, keepScalar?: boolean) => unknown;
	contents?: ParsedNode | null;
	errors: Array<{ message?: string; pos?: [number, number] }>;
	toJS: () => unknown;
};

export type YamlPositionContext = {
	path: Array<string | number>;
	parentPath: Array<string | number>;
	currentKey?: string;
	atKey: boolean;
	keyFrom?: number;
	keyTo?: number;
};

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
	if (!source || typeof source !== 'object' || Array.isArray(source)) return undefined;
	return (source as Record<string, unknown>)[key];
}

function getRange(node: unknown): [number, number] | null {
	if (!node || typeof node !== 'object') return null;
	const range = (node as { range?: [number, number, number] }).range;
	if (!range || range.length < 2) return null;
	return [range[0], range[1]];
}

function containsPosition(node: unknown, position: number): boolean {
	const range = getRange(node);
	if (!range) return false;
	return position >= range[0] && position <= range[1];
}

function scalarToKey(value: unknown): string | null {
	if (!isScalar(value)) return null;
	const scalarValue = (value as Scalar<unknown>).value;
	if (typeof scalarValue === 'string' || typeof scalarValue === 'number') {
		return String(scalarValue);
	}
	return null;
}

function findContextInNode(
	node: ParsedNode | null | undefined,
	position: number,
	path: Array<string | number>
): YamlPositionContext | null {
	if (!node) return null;
	if (!containsPosition(node, position)) return null;

	if (isMap(node)) {
		for (const pair of node.items) {
			if (!isPair(pair)) continue;

			const key = scalarToKey(pair.key);
			const keyRange = getRange(pair.key);
			if (key && keyRange && position >= keyRange[0] && position <= keyRange[1]) {
				return {
					path: [...path, key],
					parentPath: [...path],
					currentKey: key,
					atKey: true,
					keyFrom: keyRange[0],
					keyTo: keyRange[1]
				};
			}

			if (key && pair.value && containsPosition(pair.value, position)) {
				const nested = findContextInNode(pair.value as ParsedNode, position, [...path, key]);
				if (nested) return nested;
				return {
					path: [...path, key],
					parentPath: [...path],
					currentKey: key,
					atKey: false,
					keyFrom: keyRange?.[0],
					keyTo: keyRange?.[1]
				};
			}
		}

		return {
			path: [...path],
			parentPath: [...path],
			atKey: false
		};
	}

	if (isSeq(node)) {
		for (let index = 0; index < node.items.length; index += 1) {
			const item = node.items[index] as ParsedNode | null;
			if (!item) continue;
			if (!containsPosition(item, position)) continue;
			const nested = findContextInNode(item, position, [...path, index]);
			if (nested) return nested;
			return {
				path: [...path, index],
				parentPath: [...path],
				atKey: false
			};
		}
	}

	return {
		path: [...path],
		parentPath: path.slice(0, -1),
		atKey: false
	};
}

export function findYamlPositionContext(source: string, position: number): YamlPositionContext | null {
	const doc = parseDocument(source, { strict: true, uniqueKeys: false });
	return findContextInNode((doc.contents as ParsedNode | null) ?? null, position, []);
}

function escapeRegExp(value: string): string {
	return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
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

function collectDuplicateKeyDiagnostics(node: ParsedNode | null | undefined, diagnostics: Diagnostic[]): number {
	if (!node) return 0;
	let duplicateCount = 0;

	if (isMap(node)) {
		const seen = new Set<string>();
		for (const item of node.items) {
			if (!isPair(item)) continue;
			const key = scalarToKey(item.key);
			if (key) {
				const keyRange = getRange(item.key);
				if (seen.has(key)) {
					duplicateCount += 1;
					diagnostics.push({
						from: keyRange?.[0] ?? 0,
						to: Math.max((keyRange?.[0] ?? 0) + 1, keyRange?.[1] ?? 1),
						severity: 'error',
						message: `Duplicate YAML key "${key}"`
					});
				}
				seen.add(key);
			}

			duplicateCount += collectDuplicateKeyDiagnostics(item.value as ParsedNode | null, diagnostics);
		}
	}

	if (isSeq(node)) {
		for (const item of node.items) {
			duplicateCount += collectDuplicateKeyDiagnostics(item as ParsedNode | null, diagnostics);
		}
	}

	return duplicateCount;
}

function buildTabDiagnostics(source: string): Diagnostic[] {
	const diagnostics: Diagnostic[] = [];
	for (const match of source.matchAll(TAB_INDENT_REGEX)) {
		const tabs = match[2] || '';
		const newlineLength = match[1] === '\n' ? 1 : 0;
		const start = (match.index ?? 0) + newlineLength;
		diagnostics.push({
			from: start,
			to: Math.max(start + 1, start + tabs.length),
			severity: 'error',
			message: 'Tabs are not allowed for YAML indentation. Use spaces only.',
			actions: [
				{
					name: 'Convert tabs to spaces',
					apply(view: EditorView, from: number, to: number) {
						const text = view.state.doc.sliceString(from, to);
						view.dispatch({
							changes: {
								from,
								to,
								insert: text.replace(/\t/g, '  ')
							}
						});
					}
				}
			]
		});
	}

	return diagnostics;
}

function addMissingHyphenQuickFix(fieldName: string, serviceName: string, range: { from: number; to: number }): Diagnostic {
	return {
		from: range.from,
		to: range.to,
		severity: 'error',
		message: `Service "${serviceName}" ${fieldName} must be a list. Prefix each item with '-'.`,
		actions: [
			{
				name: 'Insert list marker',
				apply(view: EditorView) {
					const doc = view.state.doc;
					const fieldLineNumber = doc.lineAt(range.from).number;
					for (let current = fieldLineNumber + 1; current <= doc.lines; current += 1) {
						const line = doc.line(current);
						const trimmed = line.text.trim();
						if (!trimmed) continue;
						if (trimmed.startsWith('-')) return;
						const indent = line.text.match(/^\s*/)?.[0] ?? '';
						view.dispatch({
							changes: {
								from: line.from + indent.length,
								insert: '- '
							}
						});
						return;
					}
				}
			}
		]
	};
}

function buildComposeSemanticDiagnostics(parsedValue: unknown, doc: YamlDocLike): Diagnostic[] {
	if (!parsedValue || typeof parsedValue !== 'object' || Array.isArray(parsedValue)) return [];
	const services = getObjectValue(parsedValue, 'services');
	if (!services || typeof services !== 'object' || Array.isArray(services)) return [];

	const diagnostics: Diagnostic[] = [];

	for (const [serviceName, serviceValue] of Object.entries(services as Record<string, unknown>)) {
		if (!serviceValue || typeof serviceValue !== 'object' || Array.isArray(serviceValue)) continue;

		for (const field of LIST_FIELDS) {
			const value = getObjectValue(serviceValue, field);
			if (!value || Array.isArray(value) || typeof value !== 'object') continue;
			const range = getNodeRangeByPath(doc, ['services', serviceName, field]) || { from: 0, to: 1 };
			diagnostics.push(addMissingHyphenQuickFix(field, serviceName, range));
		}
	}

	return diagnostics;
}

function buildMissingVariableDiagnostics(
	source: string,
	context: EditorContext
): { diagnostics: Diagnostic[]; unresolved: string[] } {
	const unresolved = getMissingComposeVariables(context, source);
	if (unresolved.length === 0) return { diagnostics: [], unresolved: [] };

	const unresolvedSet = new Set(unresolved);
	const diagnostics: Diagnostic[] = [];

	for (const match of source.matchAll(VARIABLE_TOKEN_REGEX)) {
		const whole = match[0] ?? '';
		const name = match[1] || match[2];
		if (!name || !unresolvedSet.has(name)) continue;
		const from = match.index ?? 0;
		const to = Math.max(from + 1, from + whole.length);
		diagnostics.push({
			from,
			to,
			severity: 'warning',
			message: `Variable "${name}" is not defined in .env or global variables.`
		});
	}

	return { diagnostics, unresolved };
}

function buildSecretDiagnostics(source: string): Diagnostic[] {
	const diagnostics: Diagnostic[] = [];
	const lines = source.split('\n');
	let offset = 0;

	for (const rawLine of lines) {
		const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine;
		const match = /^(\s*)([A-Za-z0-9_.-]+)\s*:\s*(.+)$/.exec(line);
		if (!match) {
			offset += rawLine.length + 1;
			continue;
		}

		const key = match[2] ?? '';
		const value = (match[3] ?? '').trim();
		if (!SECRET_NAME_REGEX.test(key) && !SECRET_VALUE_REGEX.test(value)) {
			offset += rawLine.length + 1;
			continue;
		}

		const keyIndex = line.indexOf(key);
		const from = offset + Math.max(0, keyIndex);
		const to = from + Math.max(1, key.length);
		diagnostics.push({
			from,
			to,
			severity: 'warning',
			message: `"${key}" looks like a secret. Prefer Docker secrets or external secret managers.`
		});

		offset += rawLine.length + 1;
	}

	return diagnostics;
}

function buildOutline(doc: YamlDocLike): OutlineItem[] {
	const outline: OutlineItem[] = [];
	const sections = ['services', 'networks', 'volumes', 'configs', 'secrets'];

	for (const section of sections) {
		const sectionNode = doc.getIn([section], true) as { range?: [number, number, number] } | undefined;
		const sectionRange = sectionNode?.range;
		if (!sectionRange) continue;

		outline.push({
			id: `outline:${section}`,
			label: section,
			path: [section],
			from: sectionRange[0],
			to: Math.max(sectionRange[0] + 1, sectionRange[1]),
			level: 0
		});

		const value = doc.getIn([section]) as unknown;
		if (!value || typeof value !== 'object' || Array.isArray(value)) continue;

		for (const key of Object.keys(value as Record<string, unknown>)) {
			const node = doc.getIn([section, key], true) as { range?: [number, number, number] } | undefined;
			const range = node?.range;
			if (!range) continue;

			outline.push({
				id: `outline:${section}:${key}`,
				label: key,
				path: [section, key],
				from: range[0],
				to: Math.max(range[0] + 1, range[1]),
				level: 1
			});
		}
	}

	return outline;
}

export function findVariableReferenceAtPosition(
	source: string,
	position: number
): { name: string; source: 'env' | 'global' | 'missing' } | null {
	for (const match of source.matchAll(VARIABLE_TOKEN_REGEX)) {
		const name = match[1] || match[2];
		if (!name) continue;
		const whole = match[0] ?? '';
		const from = match.index ?? 0;
		const to = from + whole.length;
		if (position < from || position > to) continue;
		return { name, source: 'missing' };
	}

	return null;
}

export async function analyzeComposeContent(
	view: EditorView,
	schemaContext: ComposeSchemaContext,
	editorContext: EditorContext,
	maxSchemaDiagnostics = MAX_SCHEMA_DIAGNOSTICS_DEFAULT
): Promise<AnalysisResult> {
	const source = view.state.doc.toString();
	const lineCounter = new LineCounter();
	const doc = parseDocument(source, {
		lineCounter,
		strict: true,
		uniqueKeys: false
	}) as unknown as YamlDocLike;

	const diagnostics: Diagnostic[] = [];
	diagnostics.push(...buildTabDiagnostics(source));

	for (const error of doc.errors) {
		const start = error.pos?.[0] ?? 0;
		const end = error.pos?.[1] ?? Math.min(source.length, start + 1);
		diagnostics.push({
			from: start,
			to: Math.max(start + 1, end),
			severity: 'error',
			message: error.message || 'YAML syntax error'
		});
	}

	const duplicateKeyWarnings = collectDuplicateKeyDiagnostics(doc.contents ?? null, diagnostics);

	let outlineItems: OutlineItem[] = [];
	let unresolvedVariables: string[] = [];
	let secretWarnings = 0;

	if (diagnostics.every((item) => item.severity !== 'error')) {
		try {
			const parsedValue = doc.toJS();
			outlineItems = buildOutline(doc);

			if (schemaContext.validate) {
				const isValid = schemaContext.validate(parsedValue);
				if (!isValid) {
					for (const error of (schemaContext.validate.errors || []).slice(0, maxSchemaDiagnostics)) {
						diagnostics.push(toSchemaDiagnostic(error, doc, source));
					}
				}
			}

			diagnostics.push(...buildComposeSemanticDiagnostics(parsedValue, doc));

			const missing = buildMissingVariableDiagnostics(source, editorContext);
			unresolvedVariables = missing.unresolved;
			diagnostics.push(...missing.diagnostics);

			const secretDiagnostics = buildSecretDiagnostics(source);
			secretWarnings = secretDiagnostics.length;
			diagnostics.push(...secretDiagnostics);
		} catch {
			diagnostics.push({
				from: 0,
				to: Math.min(1, source.length || 1),
				severity: 'error',
				message: 'YAML parsing failed'
			});
		}
	}

	return {
		diagnostics,
		outlineItems,
		summaryPatch: {
			duplicateKeyWarnings,
			secretWarnings,
			unresolvedVariables
		}
	};
}

export function resolveVariableSourceAtPosition(
	source: string,
	position: number,
	editorContext: EditorContext
): { name: string; source: 'env' | 'global' | 'missing' } | null {
	const ref = findVariableReferenceAtPosition(source, position);
	if (!ref) return null;
	const resolved = resolveVariableSource(ref.name, editorContext);
	return {
		name: ref.name,
		source: resolved ?? 'missing'
	};
}
