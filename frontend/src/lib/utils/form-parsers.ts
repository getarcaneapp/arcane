/** Splits textarea content into trimmed, non-empty lines. */
export function parseLines(text: string): string[] {
	return splitAndTrim(text, '\n');
}

/** Splits newline or comma separated input into trimmed, non-empty values. */
export function parseList(text: string): string[] {
	return splitAndTrim(text, /[\n,]/);
}

function splitAndTrim(text: string, separator: string | RegExp): string[] {
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
