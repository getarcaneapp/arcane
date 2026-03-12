import type { EditorView } from '@codemirror/view';
import type { Diagnostic } from '@codemirror/lint';
import type { AnalysisResult, EditorContext, OutlineItem } from './types';
import { isOpenQuote } from './parse-env-utils';
import { extractComposeVariables } from './vars-analysis';

const ENV_KEY_REGEX = /^[A-Za-z_][A-Za-z0-9_]*$/;
const SECRET_NAME_REGEX = /(password|passwd|secret|token|api[_-]?key|private[_-]?key|credential|aws_secret)/i;
const SECRET_VALUE_REGEX = /(?:-----BEGIN [A-Z ]+-----|eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+|[A-Za-z0-9_\/-]{24,})/;

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
	duplicateKeys: number;
	secretWarnings: number;
} {
	const diagnostics: Diagnostic[] = [];
	const entries: ParsedEnvLine[] = [];
	const seen = new Map<string, ParsedEnvLine>();
	let duplicateKeys = 0;
	let secretWarnings = 0;

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

		if (seen.has(key)) {
			const previous = seen.get(key);
			duplicateKeys += 1;
			diagnostics.push({
				from: keyFrom,
				to: keyTo,
				severity: 'warning',
				message: `Duplicate variable "${key}". Last value wins.`,
				actions: [
					{
						name: 'Remove earlier duplicate',
						apply(view: EditorView) {
							const doc = view.state.doc;
							const target = previous ?? parsed;
							let removeTo = target.to;
							const nextChars = doc.sliceString(removeTo, Math.min(doc.length, removeTo + 2));
							if (nextChars.startsWith('\r\n')) {
								removeTo += 2;
							} else if (nextChars.startsWith('\n')) {
								removeTo += 1;
							}
							view.dispatch({
								changes: { from: target.from, to: removeTo, insert: '' }
							});
						}
					}
				]
			});
		}

		seen.set(key, parsed);
		entries.push(parsed);

		if (SECRET_NAME_REGEX.test(key) || SECRET_VALUE_REGEX.test(value)) {
			secretWarnings += 1;
			diagnostics.push({
				from: keyFrom,
				to: keyTo,
				severity: 'warning',
				message: `"${key}" looks like a secret. Consider using Docker secrets or external secret management.`
			});
		}
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

	return { entries, diagnostics, duplicateKeys, secretWarnings };
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

function buildUnusedVarDiagnostics(entries: ParsedEnvLine[], context: EditorContext): Diagnostic[] {
	const referenced = extractComposeVariables(context.composeContents ?? []);
	if (referenced.size === 0) return [];

	const diagnostics: Diagnostic[] = [];
	for (const entry of entries) {
		if (!referenced.has(entry.key)) {
			diagnostics.push({
				from: entry.keyFrom,
				to: entry.keyTo,
				severity: 'warning',
				message: `Variable "${entry.key}" is not referenced in compose files.`
			});
		}
	}

	return diagnostics;
}

export function analyzeEnvContent(source: string, context: EditorContext): AnalysisResult {
	const parsed = parseEnv(source);
	const unusedDiagnostics = buildUnusedVarDiagnostics(parsed.entries, context);
	const diagnostics = [...parsed.diagnostics, ...unusedDiagnostics];
	const outlineItems = makeOutlineItems(parsed.entries);

	return {
		diagnostics,
		outlineItems,
		summaryPatch: {
			duplicateEnvWarnings: parsed.duplicateKeys,
			secretWarnings: parsed.secretWarnings
		}
	};
}
