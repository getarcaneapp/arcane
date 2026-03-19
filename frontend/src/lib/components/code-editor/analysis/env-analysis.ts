import type { Diagnostic } from '@codemirror/lint';
import type { AnalysisResult, EditorContext, OutlineItem } from './types';
import { isOpenQuote } from './parse-env-utils';

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

function parseEnv(source: string): {
	entries: ParsedEnvLine[];
	diagnostics: Diagnostic[];
} {
	const diagnostics: Diagnostic[] = [];
	const entries: ParsedEnvLine[] = [];

	const lines = source.split('\n');
	let offset = 0;

	let multiLineKey: string | null = null;
	let multiLineQuote: string | null = null;
	let multiLineFrom = 0;
	let multiLineTo = 0;
	let multiLineKeyFrom = 0;
	let multiLineKeyTo = 0;
	let multiLineLineNumber = 0;
	let multiLineParts: string[] = [];

	function finalizeEntry(
		key: string,
		value: string,
		lineNumber: number,
		from: number,
		to: number,
		keyFrom: number,
		keyTo: number
	): void {
		const parsed: ParsedEnvLine = { lineNumber, from, to: Math.max(from + 1, to), key, value, keyFrom, keyTo };
		entries.push(parsed);
	}

	for (let index = 0; index < lines.length; index += 1) {
		const rawLine = lines[index] ?? '';
		const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine;
		const lineNumber = index + 1;
		const lineFrom = offset;
		const lineTo = offset + line.length;

		offset += rawLine.length + 1;

		// Inside a multi-line quoted value — accumulate until closing quote
		if (multiLineQuote !== null && multiLineKey !== null) {
			multiLineParts.push(rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine);
			multiLineTo = lineTo;

			const trimmedEnd = line.trimEnd();
			const isEscaped = trimmedEnd.length >= 2 && trimmedEnd[trimmedEnd.length - 2] === '\\';
			if (trimmedEnd.endsWith(multiLineQuote) && !isEscaped) {
				const fullValue = multiLineParts.join('\n');
				finalizeEntry(multiLineKey, fullValue, multiLineLineNumber, multiLineFrom, multiLineTo, multiLineKeyFrom, multiLineKeyTo);
				multiLineKey = null;
				multiLineQuote = null;
				multiLineParts = [];
			}
			continue;
		}

		const trimmed = line.trim();
		if (!trimmed || trimmed.startsWith('#')) continue;

		const valueLine = trimmed.startsWith('export ') ? trimmed.slice(7).trim() : trimmed;
		const separator = valueLine.indexOf('=');
		if (separator < 0) {
			diagnostics.push({
				from: lineFrom,
				to: Math.max(lineFrom + 1, lineTo),
				severity: 'error',
				message: 'Malformed .env line. Use KEY=value syntax.'
			});
			continue;
		}

		const key = valueLine.slice(0, separator).trim();
		const value = valueLine.slice(separator + 1).trim();

		const keyIndexInRaw = line.indexOf(key);
		const keyFrom = keyIndexInRaw >= 0 ? lineFrom + keyIndexInRaw : lineFrom;
		const keyTo = keyFrom + Math.max(1, key.length);

		if (!ENV_KEY_REGEX.test(key)) {
			diagnostics.push({
				from: keyFrom,
				to: keyTo,
				severity: 'error',
				message: `Invalid variable name "${key}". Use letters, numbers and underscore only.`
			});
			continue;
		}

		// Check for multi-line quoted value
		const openQuote = isOpenQuote(value);
		if (openQuote) {
			multiLineKey = key;
			multiLineQuote = openQuote;
			multiLineFrom = lineFrom;
			multiLineTo = lineTo;
			multiLineKeyFrom = keyFrom;
			multiLineKeyTo = keyTo;
			multiLineLineNumber = lineNumber;
			multiLineParts = [value];
			continue;
		}

		finalizeEntry(key, value, lineNumber, lineFrom, lineTo, keyFrom, keyTo);
	}

	// Unterminated multi-line quoted value at EOF
	if (multiLineQuote !== null && multiLineKey !== null) {
		diagnostics.push({
			from: multiLineFrom,
			to: Math.max(multiLineFrom + 1, multiLineTo),
			severity: 'error',
			message: `Unterminated quoted value for "${multiLineKey}". Missing closing ${multiLineQuote}.`
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
