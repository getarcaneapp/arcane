import Ajv, { type ValidateFunction } from 'ajv';
import type { Completion } from '@codemirror/autocomplete';
import { browser } from '$app/environment';
import type { SchemaDoc, SchemaStatus } from './types';

export const DOCKER_COMPOSE_SCHEMA_URL =
	'https://raw.githubusercontent.com/compose-spec/compose-go/refs/heads/main/schema/compose-spec.json';

const SCHEMA_CACHE_KEY = 'arcane.compose.schema.v1';

type SchemaObject = Record<string, unknown>;

type ComposeSchemaContext = {
	schema: SchemaObject | null;
	validate: ValidateFunction<unknown> | null;
	status: SchemaStatus;
	message?: string;
};

let composeSchemaPromise: Promise<ComposeSchemaContext> | null = null;
let composeSchemaContext: ComposeSchemaContext | null = null;

function asSchemaObject(value: unknown): SchemaObject | null {
	if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
	return value as SchemaObject;
}

function readCachedSchema(): SchemaObject | null {
	if (!browser) return null;
	try {
		const raw = localStorage.getItem(SCHEMA_CACHE_KEY);
		if (!raw) return null;
		const parsed = JSON.parse(raw) as unknown;
		return asSchemaObject(parsed);
	} catch {
		return null;
	}
}

function writeCachedSchema(schema: SchemaObject): void {
	if (!browser) return;
	try {
		localStorage.setItem(SCHEMA_CACHE_KEY, JSON.stringify(schema));
	} catch {
		// ignore cache write failures
	}
}

function createValidator(schema: SchemaObject): ValidateFunction<unknown> {
	const ajv = new Ajv({
		allErrors: true,
		strict: false,
		strictSchema: false,
		allowUnionTypes: true,
		validateFormats: false,
		validateSchema: false
	});

	return ajv.compile(schema);
}

function resolveRef(root: SchemaObject, ref: string, visited: Set<string>): SchemaObject | null {
	if (!ref.startsWith('#/')) return null;
	if (visited.has(ref)) return null;
	visited.add(ref);

	const segments = ref
		.slice(2)
		.split('/')
		.map((segment) => segment.replace(/~1/g, '/').replace(/~0/g, '~'));

	let current: unknown = root;
	for (const segment of segments) {
		if (!current || typeof current !== 'object') return null;
		current = (current as SchemaObject)[segment];
	}

	return asSchemaObject(current);
}

function expandCandidates(root: SchemaObject, candidate: SchemaObject, visited: Set<string>): SchemaObject[] {
	const expanded: SchemaObject[] = [];
	const ref = candidate.$ref;
	if (typeof ref === 'string') {
		const resolved = resolveRef(root, ref, visited);
		if (resolved) {
			expanded.push(...expandCandidates(root, resolved, visited));
		}
	}

	const containers = ['allOf', 'anyOf', 'oneOf'];
	for (const container of containers) {
		const node = candidate[container];
		if (Array.isArray(node)) {
			for (const item of node) {
				const asObject = asSchemaObject(item);
				if (asObject) expanded.push(...expandCandidates(root, asObject, visited));
			}
		}
	}

	expanded.push(candidate);

	const unique = new Set<SchemaObject>();
	const normalized: SchemaObject[] = [];
	for (const item of expanded) {
		if (unique.has(item)) continue;
		unique.add(item);
		normalized.push(item);
	}

	return normalized;
}

function getPathCandidates(root: SchemaObject, path: Array<string | number>): SchemaObject[] {
	let candidates: SchemaObject[] = [root];

	for (const segment of path) {
		const nextCandidates: SchemaObject[] = [];

		for (const candidate of candidates) {
			const expanded = expandCandidates(root, candidate, new Set<string>());
			for (const node of expanded) {
				if (typeof segment === 'number') {
					const prefixItems = node.prefixItems;
					if (Array.isArray(prefixItems) && segment < prefixItems.length) {
						const fromPrefix = asSchemaObject(prefixItems[segment]);
						if (fromPrefix) nextCandidates.push(fromPrefix);
					}

					const items = asSchemaObject(node.items);
					if (items) nextCandidates.push(items);
					continue;
				}

				const properties = asSchemaObject(node.properties);
				if (properties) {
					const fromProperty = asSchemaObject(properties[segment]);
					if (fromProperty) nextCandidates.push(fromProperty);
				}

				const patternProperties = asSchemaObject(node.patternProperties);
				if (patternProperties) {
					for (const patternValue of Object.values(patternProperties)) {
						const fromPattern = asSchemaObject(patternValue);
						if (fromPattern) nextCandidates.push(fromPattern);
					}
				}

				const additionalProperties = asSchemaObject(node.additionalProperties);
				if (additionalProperties) nextCandidates.push(additionalProperties);
			}
		}

		const unique = new Set<SchemaObject>();
		candidates = [];
		for (const candidate of nextCandidates) {
			if (unique.has(candidate)) continue;
			unique.add(candidate);
			candidates.push(candidate);
		}

		if (candidates.length === 0) break;
	}

	return candidates;
}

function collectPropertySchemas(root: SchemaObject, path: Array<string | number>): Map<string, SchemaObject> {
	const map = new Map<string, SchemaObject>();
	const candidates = getPathCandidates(root, path);

	for (const candidate of candidates) {
		const expanded = expandCandidates(root, candidate, new Set<string>());
		for (const node of expanded) {
			const properties = asSchemaObject(node.properties);
			if (!properties) continue;
			for (const [key, value] of Object.entries(properties)) {
				const propertySchema = asSchemaObject(value);
				if (propertySchema) {
					map.set(key, propertySchema);
				}
			}
		}
	}

	return map;
}

function extractSchemaDoc(schema: SchemaObject): SchemaDoc {
	const title = typeof schema.title === 'string' ? schema.title : undefined;
	const description = typeof schema.description === 'string' ? schema.description : undefined;
	const defaultValue = schema.default !== undefined ? JSON.stringify(schema.default) : undefined;
	let examples: string[] | undefined;

	if (Array.isArray(schema.examples)) {
		examples = schema.examples.slice(0, 3).map((value) => JSON.stringify(value));
	}

	return {
		title,
		description,
		defaultValue,
		examples
	};
}

export async function getComposeSchemaContext(): Promise<ComposeSchemaContext> {
	if (composeSchemaContext) return composeSchemaContext;
	if (composeSchemaPromise) return composeSchemaPromise;

	composeSchemaPromise = (async () => {
		try {
			const response = await fetch(DOCKER_COMPOSE_SCHEMA_URL, { cache: 'no-store' });
			if (!response.ok) {
				throw new Error(`HTTP ${response.status}`);
			}

			const payload = (await response.json()) as unknown;
			const schema = asSchemaObject(payload);
			if (!schema) throw new Error('Invalid compose schema payload');

			writeCachedSchema(schema);
			composeSchemaContext = {
				schema,
				validate: createValidator(schema),
				status: 'ready'
			};
			return composeSchemaContext;
		} catch (error) {
			const cached = readCachedSchema();
			if (cached) {
				composeSchemaContext = {
					schema: cached,
					validate: createValidator(cached),
					status: 'cached',
					message: 'Using cached Docker Compose schema'
				};
				return composeSchemaContext;
			}

			composeSchemaContext = {
				schema: null,
				validate: null,
				status: 'unavailable',
				message: error instanceof Error ? error.message : 'Schema unavailable'
			};
			return composeSchemaContext;
		} finally {
			composeSchemaPromise = null;
		}
	})();

	return composeSchemaPromise;
}

export function getCompletionOptionsForPath(
	schema: SchemaObject | null,
	path: Array<string | number>,
	prefix = ''
): Completion[] {
	if (!schema) return [];
	const map = collectPropertySchemas(schema, path);
	const normalizedPrefix = prefix.toLowerCase();

	return Array.from(map.entries())
		.filter(([key]) => key.toLowerCase().includes(normalizedPrefix))
		.sort(([a], [b]) => a.localeCompare(b))
		.map(([key]) => {
			const doc = extractSchemaDoc(map.get(key) ?? {});
			return {
				label: key,
				type: 'property',
				detail: doc.title,
				info: doc.description,
				apply: `${key}: `
			} as Completion;
		});
}

export function getEnumValueCompletions(schema: SchemaObject | null, path: Array<string | number>): Completion[] {
	if (!schema) return [];
	const candidates = getPathCandidates(schema, path);
	const values = new Set<string>();

	for (const candidate of candidates) {
		const expanded = expandCandidates(schema, candidate, new Set<string>());
		for (const node of expanded) {
			if (!Array.isArray(node.enum)) continue;
			for (const value of node.enum) {
				if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
					values.add(String(value));
				}
			}
		}
	}

	return Array.from(values)
		.sort((a, b) => a.localeCompare(b))
		.map((value) => ({
			label: value,
			type: 'enum',
			apply: /^\d+$/.test(value) || value === 'true' || value === 'false' ? value : JSON.stringify(value)
		}));
}

export function getSchemaDocForPath(schema: SchemaObject | null, path: Array<string | number>): SchemaDoc | null {
	if (!schema) return null;
	const candidates = getPathCandidates(schema, path);
	for (const candidate of candidates) {
		const doc = extractSchemaDoc(candidate);
		if (doc.title || doc.description || doc.defaultValue || (doc.examples && doc.examples.length > 0)) {
			return doc;
		}
	}
	return null;
}

export type { ComposeSchemaContext, SchemaObject };
