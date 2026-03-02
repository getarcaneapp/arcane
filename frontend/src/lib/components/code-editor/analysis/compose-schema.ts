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

type ArcaneCompletionSpec = {
	label: string;
	detail: string;
	info: string;
};

let composeSchemaPromise: Promise<ComposeSchemaContext> | null = null;
let composeSchemaContext: ComposeSchemaContext | null = null;

const ARCANE_ROOT_EXTENSION_COMPLETIONS: ArcaneCompletionSpec[] = [
	{
		label: 'x-arcane',
		detail: 'Arcane extension',
		info: 'Arcane compose metadata block for project-level icon and URLs.'
	},
	{
		label: 'x-arcane-icon',
		detail: 'Arcane extension helper',
		info: 'Optional extension key often used for YAML anchors/reuse of icon values.'
	}
];

const ARCANE_BLOCK_COMPLETIONS: ArcaneCompletionSpec[] = [
	{
		label: 'icon',
		detail: 'Arcane project icon',
		info: 'Project icon URL or icon identifier.'
	},
	{
		label: 'icons',
		detail: 'Arcane project icon (alias)',
		info: 'Alias for icon metadata; first non-empty value is used.'
	},
	{
		label: 'urls',
		detail: 'Arcane project URLs',
		info: 'Additional project URLs (for example docs or homepage links).'
	}
];

const ARCANE_SERVICE_EXTENSION_COMPLETIONS: ArcaneCompletionSpec[] = [
	{
		label: 'x-arcane',
		detail: 'Arcane service extension',
		info: 'Arcane service metadata block for service icon overrides.'
	}
];

const ARCANE_SERVICE_BLOCK_COMPLETIONS: ArcaneCompletionSpec[] = [
	{
		label: 'icon',
		detail: 'Arcane service icon',
		info: 'Service icon URL or icon identifier.'
	},
	{
		label: 'icons',
		detail: 'Arcane service icon (alias)',
		info: 'Alias for service icon metadata; first non-empty value is used.'
	}
];

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

function isRootPath(path: Array<string | number>): boolean {
	return path.length === 0;
}

function isRootArcanePath(path: Array<string | number>): boolean {
	return path.length === 1 && path[0] === 'x-arcane';
}

function isServicePath(path: Array<string | number>): boolean {
	return path.length === 2 && path[0] === 'services' && typeof path[1] === 'string';
}

function isServiceArcanePath(path: Array<string | number>): boolean {
	return path.length === 3 && path[0] === 'services' && typeof path[1] === 'string' && path[2] === 'x-arcane';
}

function toArcaneCompletion(spec: ArcaneCompletionSpec): Completion {
	return {
		label: spec.label,
		type: 'property',
		detail: spec.detail,
		info: spec.info,
		apply: `${spec.label}: `
	};
}

function getArcaneCompletionOptionsForPath(path: Array<string | number>, prefix = ''): Completion[] {
	const normalizedPrefix = prefix.toLowerCase();
	let specs: ArcaneCompletionSpec[] = [];

	if (isRootPath(path)) {
		specs = ARCANE_ROOT_EXTENSION_COMPLETIONS;
	} else if (isRootArcanePath(path)) {
		specs = ARCANE_BLOCK_COMPLETIONS;
	} else if (isServicePath(path)) {
		specs = ARCANE_SERVICE_EXTENSION_COMPLETIONS;
	} else if (isServiceArcanePath(path)) {
		specs = ARCANE_SERVICE_BLOCK_COMPLETIONS;
	}

	return specs
		.filter((item) => item.label.toLowerCase().includes(normalizedPrefix))
		.sort((a, b) => a.label.localeCompare(b.label))
		.map(toArcaneCompletion);
}

function getArcaneSchemaDocForPath(path: Array<string | number>): SchemaDoc | null {
	if (path.length === 1 && path[0] === 'x-arcane') {
		return {
			title: 'x-arcane',
			description: 'Arcane extension block for project-level metadata such as icon and custom URLs.'
		};
	}

	if (path.length === 1 && path[0] === 'x-arcane-icon') {
		return {
			title: 'x-arcane-icon',
			description: 'Optional Arcane helper extension key, commonly used as a YAML anchor value for icon reuse.'
		};
	}

	if (path.length === 2 && path[0] === 'x-arcane' && path[1] === 'icon') {
		return {
			title: 'x-arcane.icon',
			description: 'Project icon URL or icon identifier used for the stack/project display.'
		};
	}

	if (path.length === 2 && path[0] === 'x-arcane' && path[1] === 'icons') {
		return {
			title: 'x-arcane.icons',
			description: 'Alias for icon metadata. Arcane uses the first non-empty value between icon/icons.'
		};
	}

	if (path.length === 2 && path[0] === 'x-arcane' && path[1] === 'urls') {
		return {
			title: 'x-arcane.urls',
			description: 'Additional project URLs (for example docs, homepage, or dashboards).'
		};
	}

	if (path.length === 3 && path[0] === 'services' && typeof path[1] === 'string' && path[2] === 'x-arcane') {
		return {
			title: 'services.<name>.x-arcane',
			description: 'Arcane extension block for service-level metadata.'
		};
	}

	if (
		path.length === 4 &&
		path[0] === 'services' &&
		typeof path[1] === 'string' &&
		path[2] === 'x-arcane' &&
		(path[3] === 'icon' || path[3] === 'icons')
	) {
		return {
			title: `services.<name>.x-arcane.${String(path[3])}`,
			description: 'Service icon URL or icon identifier override.'
		};
	}

	return null;
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
	const normalizedPrefix = prefix.toLowerCase();
	const propertyMap = schema ? collectPropertySchemas(schema, path) : new Map<string, SchemaObject>();
	const schemaCompletions = Array.from(propertyMap.entries())
		.filter(([key]) => key.toLowerCase().includes(normalizedPrefix))
		.sort(([a], [b]) => a.localeCompare(b))
		.map(([key, propertySchema]) => {
			const doc = extractSchemaDoc(propertySchema);
			return {
				label: key,
				type: 'property',
				detail: doc.title,
				info: doc.description,
				apply: `${key}: `
			} as Completion;
		});

	const arcaneCompletions = getArcaneCompletionOptionsForPath(path, prefix);
	const merged = new Map<string, Completion>();

	for (const completion of schemaCompletions) {
		merged.set(completion.label, completion);
	}

	for (const completion of arcaneCompletions) {
		if (!merged.has(completion.label)) {
			merged.set(completion.label, completion);
		}
	}

	return Array.from(merged.values()).sort((a, b) => a.label.localeCompare(b.label));
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
	const arcaneDoc = getArcaneSchemaDocForPath(path);
	if (!schema) return arcaneDoc;
	const candidates = getPathCandidates(schema, path);
	for (const candidate of candidates) {
		const doc = extractSchemaDoc(candidate);
		if (doc.title || doc.description || doc.defaultValue || (doc.examples && doc.examples.length > 0)) {
			return doc;
		}
	}
	return arcaneDoc;
}

export type { ComposeSchemaContext, SchemaObject };
