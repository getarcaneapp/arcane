import type { Diagnostic } from '@codemirror/lint';
import type { AnalysisResult, EditorContext, OutlineItem } from './types';
import { hasClosingQuote, isOpenQuote } from './parse-env-utils';

const ENV_KEY_REGEX = /^[A-Za-z_][A-Za-z0-9_]*$/;

type ParsedEnvLine = {
	lineNumber: number;
	from: number;
	to: number;
	key: string;
	value: string;
	keyFrom: number;
	keyTo: number;
};

function parseEnvLine(line: string, lineNumber: number, from: number, diagnostics: Diagnostic[]): ParsedEnvLine | null {
	const trimmed = line.trim();
	if (!trimmed || trimmed.startsWith('#')) return null;
	const valueLine = trimmed.startsWith('export ') ? trimmed.slice(7).trim() : trimmed;
	const separator = valueLine.indexOf('=');
	const to = Math.max(from + 1, from + line.length);
	if (separator < 0) {
		diagnostics.push({ from, to, severity: 'error', message: 'Malformed .env line. Use KEY=value syntax.' });
		return null;
	}
	const key = valueLine.slice(0, separator).trim();
	const value = valueLine.slice(separator + 1).trim();
	const keyIndex = line.indexOf(key);
	const keyFrom = keyIndex >= 0 ? from + keyIndex : from;
	const keyTo = keyFrom + Math.max(1, key.length);
	if (!ENV_KEY_REGEX.test(key)) {
		diagnostics.push({
			from: keyFrom,
			to: keyTo,
			severity: 'error',
			message: `Invalid variable name "${key}". Use letters, numbers and underscore only.`
		});
		return null;
	}
	return { lineNumber, from, to, key, value, keyFrom, keyTo };
}

function parseEnv(source: string): { entries: ParsedEnvLine[]; diagnostics: Diagnostic[] } {
	const diagnostics: Diagnostic[] = [];
	const entries: ParsedEnvLine[] = [];
	let pending: { entry: ParsedEnvLine; quote: string; parts: string[] } | null = null;
	let offset = 0;
	const lines = source.split('\n');
	for (const [index, rawLine] of lines.entries()) {
		const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine;
		const from = offset;
		offset += rawLine.length + 1;
		if (pending) {
			pending.parts.push(line);
			pending.entry.to = Math.max(pending.entry.from + 1, from + line.length);
			if (hasClosingQuote(line, pending.quote)) {
				entries.push({ ...pending.entry, value: pending.parts.join('\n') });
				pending = null;
			}
			continue;
		}
		const entry = parseEnvLine(line, index + 1, from, diagnostics);
		if (!entry) continue;
		const quote = isOpenQuote(entry.value);
		if (quote) {
			pending = { entry, quote, parts: [entry.value] };
		} else {
			entries.push(entry);
		}
	}
	if (pending) {
		diagnostics.push({
			from: pending.entry.from,
			to: pending.entry.to,
			severity: 'error',
			message: `Unterminated quoted value for "${pending.entry.key}". Missing closing ${pending.quote}.`
		});
	}
	return { entries, diagnostics };
}

function makeOutlineItems(entries: ParsedEnvLine[]): OutlineItem[] {
	return entries.map((entry) => ({
		id: `env:${entry.key}:${entry.lineNumber}`,
		label: entry.key,
		path: [entry.key],
		from: entry.keyFrom,
		to: entry.keyTo,
		level: 0
	}));
}

export function analyzeEnvContent(source: string, _context: EditorContext): AnalysisResult {
	const parsed = parseEnv(source);
	const diagnostics = [...parsed.diagnostics];
	const outlineItems = makeOutlineItems(parsed.entries);

	return {
		diagnostics,
		outlineItems,
		summaryPatch: {}
	};
}
