import { sanitizeLogText } from '#lib/utils/formatting.js';

export interface LogViewerEntry {
	id: number;
	timestamp: string;
	level: 'stdout' | 'stderr' | 'info' | 'error';
	message: string;
	html: string;
	service?: string;
	containerId?: string;
	structured?: StructuredLogResult;
}

export interface StructuredLogResult {
	isJson: boolean;
	isStructured: boolean;
	parsed?: Record<string, unknown> | unknown[];
}

export interface LogViewerDisplayEntry {
	id: number;
	key: string;
	grouped: boolean;
	entries: LogViewerEntry[];
}

export function buildLogDisplayEntries(
	logs: LogViewerEntry[],
	options: {
		groupAdjacentLines?: boolean;
		type: 'container' | 'project' | 'service';
	}
): LogViewerDisplayEntry[] {
	if (!options.groupAdjacentLines || options.type !== 'container') {
		return logs.map((entry) => ({
			id: entry.id,
			key: `single:${entry.id}`,
			grouped: false,
			entries: [entry]
		}));
	}

	const displayEntries: LogViewerDisplayEntry[] = [];

	for (const entry of logs) {
		const previous = displayEntries.at(-1);
		const lastEntry = previous?.entries.at(-1);

		if (previous && lastEntry && canGroupLogEntries(lastEntry, entry)) {
			previous.entries.push(entry);
			previous.grouped = true;
			continue;
		}

		displayEntries.push({
			id: entry.id,
			key: `single:${entry.id}`,
			grouped: false,
			entries: [entry]
		});
	}

	for (const displayEntry of displayEntries) {
		if (displayEntry.grouped) {
			displayEntry.key = `group:${displayEntry.id}`;
		}
	}

	return displayEntries;
}

function canGroupLogEntries(previous: LogViewerEntry, current: LogViewerEntry): boolean {
	return (
		!!previous.containerId &&
		previous.containerId === current.containerId &&
		previous.level === current.level &&
		looksLikeMultilineContinuation(previous.message, current.message) &&
		!getStructuredLog(previous).isStructured &&
		!getStructuredLog(current).isStructured
	);
}

function looksLikeMultilineContinuation(previousMessage: string, currentMessage: string): boolean {
	const previousLine = sanitizeLogText(previousMessage);
	const currentLine = sanitizeLogText(currentMessage);
	const currentTrimmed = currentLine.trimStart();

	if (!currentTrimmed) {
		return true;
	}

	if (/^\s+/.test(currentLine)) {
		return true;
	}

	if (/^[|│]/.test(currentTrimmed)) {
		return true;
	}

	if (
		/^(Traceback\b|File\s+"|Caused by:|During handling of the above exception|The above exception was the direct cause)/.test(
			currentTrimmed
		)
	) {
		return true;
	}

	if (/^(at\s+\S|\.\.\. \d+ more$|[~^]+$)/.test(currentTrimmed)) {
		return true;
	}

	if (previousLine.endsWith(':') && /^(Traceback\b|File\s+"|[A-Z][a-zA-Z]+(?:Error|Exception)\b)/.test(currentTrimmed)) {
		return true;
	}

	return false;
}

function unwrapWrappedJson(text: string): string {
	let candidate = text.trim();

	while (candidate.length >= 2) {
		const first = candidate[0];
		const last = candidate.at(-1);

		if (first === '(' && last === ')') {
			candidate = candidate.slice(1, -1).trim();
			continue;
		}

		break;
	}

	return candidate;
}

function readQuotedValue(input: string, start: number): { value: string; nextIndex: number } | null {
	const quote = input[start];
	if (quote !== '"' && quote !== "'") {
		return null;
	}

	let value = '';
	let index = start + 1;
	let escaped = false;

	for (; index < input.length; index++) {
		const char = input[index];
		if (char === undefined) {
			break;
		}

		if (escaped) {
			value += char;
			escaped = false;
			continue;
		}

		if (char === '\\') {
			escaped = true;
			continue;
		}

		if (char === quote) {
			return { value, nextIndex: index + 1 };
		}

		value += char;
	}

	return null;
}

function readUnquotedValue(input: string, start: number): { value: string; nextIndex: number } {
	let index = start;

	for (; index < input.length; index++) {
		const char = input[index];
		if (char === undefined || /\s/.test(char)) {
			break;
		}
	}

	return {
		value: input.slice(start, index),
		nextIndex: index
	};
}

function coerceStructuredValue(value: string): unknown {
	const trimmed = value.trim();
	if (!trimmed) return '';
	if (trimmed === 'true') return true;
	if (trimmed === 'false') return false;
	if (trimmed === 'null') return null;
	if (/^-?\d+(?:\.\d+)?$/.test(trimmed)) {
		const numeric = Number(trimmed);
		if (!Number.isNaN(numeric)) {
			return numeric;
		}
	}

	return trimmed;
}

function tryParseLogfmt(message: string): Record<string, unknown> | null {
	const trimmed = message.trim();
	if (!trimmed || !trimmed.includes('=')) {
		return null;
	}

	const parsed: Record<string, unknown> = {};
	let index = 0;

	while (index < trimmed.length) {
		while (index < trimmed.length && /\s/.test(trimmed[index]!)) {
			index++;
		}

		if (index >= trimmed.length) {
			break;
		}

		const keyStart = index;
		while (index < trimmed.length) {
			const char = trimmed[index];
			if (char === undefined || char === '=' || /\s/.test(char)) {
				break;
			}
			index++;
		}

		const key = trimmed.slice(keyStart, index);
		if (!key) {
			return null;
		}

		if (trimmed[index] !== '=') {
			return null;
		}

		index++;
		if (index >= trimmed.length) {
			parsed[key] = '';
			break;
		}

		const next = trimmed[index];
		const valueResult = next === '"' || next === "'" ? readQuotedValue(trimmed, index) : readUnquotedValue(trimmed, index);

		if (!valueResult) {
			return null;
		}

		parsed[key] = coerceStructuredValue(valueResult.value);
		index = valueResult.nextIndex;
	}

	return Object.keys(parsed).length > 0 ? parsed : null;
}

export function getStructuredLog(entry: LogViewerEntry): StructuredLogResult {
	entry.structured ??= tryParseStructuredLog(entry.message);
	return entry.structured;
}

function tryParseStructuredLog(message: string): StructuredLogResult {
	const trimmed = sanitizeLogText(message).trim();
	if (!trimmed) {
		return { isJson: false, isStructured: false };
	}

	let jsonCandidate = unwrapWrappedJson(trimmed);
	if (!jsonCandidate.startsWith('{') && !jsonCandidate.startsWith('[')) {
		let prefixEnd = 0;
		while (prefixEnd < jsonCandidate.length && jsonCandidate.charAt(prefixEnd).trim() !== '') {
			prefixEnd++;
		}

		const payload = jsonCandidate.slice(prefixEnd).trimStart();
		if (payload.startsWith('{') || payload.startsWith('[')) {
			const hasDetailsPrefix = jsonCandidate
				.slice(0, prefixEnd)
				.split(',')
				.every((attribute) => {
					const equalsIndex = attribute.indexOf('=');
					return equalsIndex > 0 && equalsIndex === attribute.lastIndexOf('=');
				});
			if (hasDetailsPrefix) {
				jsonCandidate = payload;
			}
		}
	}
	if (jsonCandidate.startsWith('{') || jsonCandidate.startsWith('[')) {
		try {
			const parsed = JSON.parse(jsonCandidate);
			if (parsed && typeof parsed === 'object') {
				return {
					isJson: true,
					isStructured: true,
					parsed
				};
			}
		} catch {
			// Fall through to logfmt parsing.
		}
	}

	const logfmtParsed = tryParseLogfmt(trimmed);
	if (logfmtParsed) {
		return {
			isJson: false,
			isStructured: true,
			parsed: logfmtParsed
		};
	}

	return { isJson: false, isStructured: false };
}

type LogLevelTone = 'info' | 'warn' | 'error';

const LOG_LEVEL_TONE_CLASSES: Record<LogLevelTone, string> = {
	info: 'text-info',
	warn: 'text-warning',
	error: 'text-destructive'
};

const BRACKETED_LOG_LEVELS: [string, LogLevelTone][] = (
	[
		['[INF]', 'info'],
		['[INFO]', 'info'],
		['[WRN]', 'warn'],
		['[WARN]', 'warn'],
		['[WARNING]', 'warn'],
		['[ERR]', 'error'],
		['[ERROR]', 'error'],
		['[FATAL]', 'error']
	] as [string, LogLevelTone][]
).flatMap(([token, tone]) => [
	[token, tone],
	[token.toLowerCase(), tone]
]);

const WORD_LOG_LEVELS: [string, LogLevelTone][] = [
	['INFO', 'info'],
	['WARN', 'warn'],
	['WARNING', 'warn'],
	['ERROR', 'error'],
	['FATAL', 'error']
];

const LOG_LEVEL_VALUES: Record<string, LogLevelTone> = {
	info: 'info',
	warn: 'warn',
	warning: 'warn',
	err: 'error',
	error: 'error',
	fatal: 'error',
	panic: 'error',
	crit: 'error',
	critical: 'error'
};

interface LogLevelMatch {
	start: number;
	end: number;
	tone: LogLevelTone;
}

function isWordChar(char: string | undefined): boolean {
	if (!char) return false;
	return char.toLowerCase() !== char.toUpperCase() || (char >= '0' && char <= '9') || char === '_';
}

function isInsideTag(html: string, index: number): boolean {
	return html.lastIndexOf('<', index) > html.lastIndexOf('>', index);
}

function findLogToken(html: string, token: string, wholeWord: boolean): number {
	let index = html.indexOf(token);
	while (index !== -1) {
		const bounded = !wholeWord || (!isWordChar(html[index - 1]) && !isWordChar(html[index + token.length]));
		if (bounded && !isInsideTag(html, index)) return index;
		index = html.indexOf(token, index + token.length);
	}
	return -1;
}

function findLevelAssignment(html: string): LogLevelMatch | null {
	const key = 'level=';
	const index = findLogToken(html, key, false);
	if (index === -1 || isWordChar(html[index - 1])) return null;

	const quote = '&quot;';
	const start = index + key.length + (html.startsWith(quote, index + key.length) ? quote.length : 0);
	let end = start;
	while (isWordChar(html[end])) end++;

	const tone = LOG_LEVEL_VALUES[html.slice(start, end).toLowerCase()];
	return tone ? { start, end, tone } : null;
}

function findLogLevel(html: string): LogLevelMatch | null {
	const candidates: LogLevelMatch[] = [];
	for (const [token, tone] of BRACKETED_LOG_LEVELS) {
		const start = findLogToken(html, token, false);
		if (start !== -1) candidates.push({ start, end: start + token.length, tone });
	}
	for (const [token, tone] of WORD_LOG_LEVELS) {
		const start = findLogToken(html, token, true);
		if (start !== -1) candidates.push({ start, end: start + token.length, tone });
	}
	const assignment = findLevelAssignment(html);
	if (assignment) candidates.push(assignment);

	return candidates.reduce<LogLevelMatch | null>(
		(earliest, match) => (!earliest || match.start < earliest.start ? match : earliest),
		null
	);
}

export function tintLogLevel(html: string): string {
	const match = findLogLevel(html);
	if (!match) return html;
	return `${html.slice(0, match.start)}<span class="${LOG_LEVEL_TONE_CLASSES[match.tone]}">${html.slice(match.start, match.end)}</span>${html.slice(match.end)}`;
}
