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
