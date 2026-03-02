import type { EditorView } from '@codemirror/view';
import type { Diagnostic } from '@codemirror/lint';
import type { AnalysisResult, EditorContext, OutlineItem } from './types';
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

	for (let index = 0; index < lines.length; index += 1) {
		const rawLine = lines[index] ?? '';
		const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine;
		const lineNumber = index + 1;
		const lineFrom = offset;
		const lineTo = offset + line.length;

		offset += rawLine.length + 1;

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

		const parsed: ParsedEnvLine = {
			lineNumber,
			from: lineFrom,
			to: Math.max(lineFrom + 1, lineTo),
			key,
			value,
			keyFrom,
			keyTo
		};

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
							const targetLineNumber = previous?.lineNumber ?? lineNumber;
							if (targetLineNumber < 1 || targetLineNumber > doc.lines) return;
							const lineInfo = doc.line(targetLineNumber);
							const removeTo = lineInfo.number < doc.lines ? doc.line(targetLineNumber + 1).from : lineInfo.to;
							view.dispatch({
								changes: { from: lineInfo.from, to: removeTo, insert: '' }
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
