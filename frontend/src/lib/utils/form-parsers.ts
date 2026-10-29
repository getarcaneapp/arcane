import type { KeyValueRow } from '#lib/components/form/key-value-editor.svelte';

/** Splits textarea content into trimmed, non-empty lines. */
export function parseLines(text: string): string[] {
	return splitAndTrim(text, '\n');
}

/** Splits newline or comma separated input into trimmed, non-empty values. */
export function parseList(text: string): string[] {
	return splitAndTrim(text.replaceAll(',', '\n'), '\n');
}

function splitAndTrim(text: string, separator: string): string[] {
	if (!text) return [];
	return text
		.split(separator)
		.map((value) => value.trim())
		.filter(Boolean);
}

export function parseKeyValuePairs(text: string): Record<string, string> {
	if (!text?.trim()) return {};

	const result: Record<string, string> = {};
	const lines = text.split('\n');

	for (const line of lines) {
		const trimmed = line.trim();
		if (!trimmed || !trimmed.includes('=')) continue;

		const [key, ...valueParts] = trimmed.split('=');
		const value = valueParts.join('=');

		if (key?.trim()) {
			result[key.trim()] = value.trim();
		}
	}

	return result;
}

export function keyValueRowsToRecord(rows: KeyValueRow[]): Record<string, string> {
	return Object.fromEntries(rows.filter((row) => row.key.trim()).map((row) => [row.key.trim(), row.value]));
}

export function recordToKeyValueRows(record: Record<string, string> | null | undefined): KeyValueRow[] {
	return Object.entries(record ?? {}).map(([key, value]) => ({ key, value }));
}
