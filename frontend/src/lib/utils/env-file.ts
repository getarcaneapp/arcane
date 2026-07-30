export interface ParsedEnvEntry {
	key: string;
	value: string;
}

export interface ParsedEnvResult {
	entries: ParsedEnvEntry[];
	invalidLines: number[]; // 1-based line numbers that could not be parsed
}

const envKeyPattern = /^[A-Za-z_][A-Za-z0-9_]*$/;

/**
 * Parses dotenv-style text: skips blanks and # comments, strips an optional
 * `export ` prefix, splits on the first `=`, unquotes single/double-quoted
 * values, and strips unquoted trailing comments. Later duplicate keys are kept
 * as separate entries so callers can surface duplicates instead of silently
 * keeping one.
 */
export function parseEnvText(text: string): ParsedEnvResult {
	const entries: ParsedEnvEntry[] = [];
	const invalidLines: number[] = [];

	const lines = text.split(/\r?\n/);
	for (let i = 0; i < lines.length; i++) {
		let line = (lines[i] ?? '').trim();
		if (line === '' || line.startsWith('#')) continue;
		if (line.startsWith('export ')) line = line.slice('export '.length).trimStart();

		const eqIndex = line.indexOf('=');
		if (eqIndex <= 0) {
			invalidLines.push(i + 1);
			continue;
		}

		const key = line.slice(0, eqIndex).trim();
		if (!envKeyPattern.test(key)) {
			invalidLines.push(i + 1);
			continue;
		}

		let value = line.slice(eqIndex + 1).trim();
		if (value.length >= 2 && ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'")))) {
			value = value.slice(1, -1);
		} else {
			const commentIndex = value.indexOf(' #');
			if (commentIndex >= 0) value = value.slice(0, commentIndex).trimEnd();
		}

		entries.push({ key: key.toUpperCase(), value });
	}

	return { entries, invalidLines };
}

/**
 * Input handler for variable-key fields: uppercases and replaces whitespace
 * with underscores while preserving the cursor position.
 */
export function normalizeVariableKeyInput(event: Event, apply: (value: string) => void): void {
	const target = event.target as HTMLInputElement;
	const cursorPos = target.selectionStart ?? 0;
	const oldLength = target.value.length;
	const newValue = target.value.toUpperCase().replace(/\s/g, '_');

	apply(newValue);

	requestAnimationFrame(() => {
		const diff = newValue.length - oldLength;
		target.setSelectionRange(cursorPos + diff, cursorPos + diff);
	});
}
