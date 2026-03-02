import type { EditorContext } from './types';

const BRACED_VAR_REGEX = /\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(?::[-?+])[^}]*)?\}/g;
const SIMPLE_VAR_REGEX = /(^|[^$])\$([A-Za-z_][A-Za-z0-9_]*)/g;
const ENV_KEY_REGEX = /^[A-Za-z_][A-Za-z0-9_]*$/;

export type VariableSource = 'env' | 'global';

export function extractComposeVariables(contents: string[]): Set<string> {
	const vars = new Set<string>();

	for (const content of contents) {
		for (const match of content.matchAll(BRACED_VAR_REGEX)) {
			if (match[1]) vars.add(match[1]);
		}

		for (const match of content.matchAll(SIMPLE_VAR_REGEX)) {
			if (match[2]) vars.add(match[2]);
		}
	}

	return vars;
}

export function parseEnvVariables(envContent: string): Map<string, string> {
	const values = new Map<string, string>();
	const lines = envContent.split(/\r?\n/);

	for (const rawLine of lines) {
		const trimmed = rawLine.trim();
		if (!trimmed || trimmed.startsWith('#')) continue;

		const line = trimmed.startsWith('export ') ? trimmed.slice(7).trim() : trimmed;
		const separator = line.indexOf('=');
		if (separator < 0) continue;

		const key = line.slice(0, separator).trim();
		if (!ENV_KEY_REGEX.test(key)) continue;

		const value = line.slice(separator + 1).trim();
		values.set(key, value);
	}

	return values;
}

export function buildVariableSourceMap(context: EditorContext): Map<string, VariableSource> {
	const source = new Map<string, VariableSource>();
	const envValues = parseEnvVariables(context.envContent ?? '');

	for (const key of envValues.keys()) {
		source.set(key, 'env');
	}

	for (const key of Object.keys(context.globalVariables ?? {})) {
		if (!source.has(key)) {
			source.set(key, 'global');
		}
	}

	return source;
}

export function resolveVariableSource(variableName: string, context: EditorContext): VariableSource | null {
	const source = buildVariableSourceMap(context);
	return source.get(variableName) ?? null;
}

export function getMissingComposeVariables(context: EditorContext, activeComposeContent: string): string[] {
	const composeContents = [activeComposeContent, ...(context.composeContents ?? [])].filter((item) => item.length > 0);
	const referenced = extractComposeVariables(composeContents);
	const available = new Set<string>([
		...parseEnvVariables(context.envContent ?? '').keys(),
		...Object.keys(context.globalVariables ?? {})
	]);

	const missing: string[] = [];
	for (const name of referenced) {
		if (!available.has(name)) {
			missing.push(name);
		}
	}

	return missing.sort((a, b) => a.localeCompare(b));
}

export function getUnusedEnvVariables(context: EditorContext): string[] {
	const envValues = parseEnvVariables(context.envContent ?? '');
	if (!envValues.size) return [];

	const referenced = extractComposeVariables(context.composeContents ?? []);

	const unused: string[] = [];
	for (const key of envValues.keys()) {
		if (!referenced.has(key)) {
			unused.push(key);
		}
	}

	return unused.sort((a, b) => a.localeCompare(b));
}
