import type { EditorContext } from './types';
import { isOpenQuote } from './parse-env-utils';

const ENV_KEY_REGEX = /^[A-Za-z_][A-Za-z0-9_]*$/;

export type VariableSource = 'env' | 'global';

function parseEnvVariables(envContent: string): Map<string, string> {
	const values = new Map<string, string>();
	const lines = envContent.split(/\r?\n/);

	let multiLineKey: string | null = null;
	let multiLineQuote: string | null = null;
	let multiLineParts: string[] = [];

	for (const rawLine of lines) {
		// Inside a multi-line quoted value — accumulate until closing quote
		if (multiLineQuote !== null && multiLineKey !== null) {
			multiLineParts.push(rawLine);
			const trimmedEnd = rawLine.trimEnd();
			const isEscaped = trimmedEnd.length >= 2 && trimmedEnd[trimmedEnd.length - 2] === '\\';
			if (trimmedEnd.endsWith(multiLineQuote) && !isEscaped) {
				values.set(multiLineKey, multiLineParts.join('\n'));
				multiLineKey = null;
				multiLineQuote = null;
				multiLineParts = [];
			}
			continue;
		}

		const trimmed = rawLine.trim();
		if (!trimmed || trimmed.startsWith('#')) continue;

		const line = trimmed.startsWith('export ') ? trimmed.slice(7).trim() : trimmed;
		const separator = line.indexOf('=');
		if (separator < 0) continue;

		const key = line.slice(0, separator).trim();
		if (!ENV_KEY_REGEX.test(key)) continue;

		const value = line.slice(separator + 1).trim();

		const openQuote = isOpenQuote(value);
		if (openQuote) {
			multiLineKey = key;
			multiLineQuote = openQuote;
			multiLineParts = [value];
			continue;
		}

		values.set(key, value);
	}

	if (multiLineKey !== null && multiLineParts.length > 0) {
		values.set(multiLineKey, multiLineParts.join('\n'));
	}

	return values;
}

function buildVariableSourceMap(context: EditorContext): Map<string, VariableSource> {
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
