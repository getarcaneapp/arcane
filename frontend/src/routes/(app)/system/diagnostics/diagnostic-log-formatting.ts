function isRecord(value: unknown): value is Record<string, unknown> {
	return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function formatAttrValue(value: unknown): string {
	if (value === undefined) return '';
	if (value === null) return 'null';
	if (typeof value === 'string') return value;
	if (typeof value === 'number' || typeof value === 'boolean' || typeof value === 'bigint') {
		return String(value);
	}
	try {
		return JSON.stringify(value);
	} catch {
		return String(value);
	}
}

function appendAttrParts(prefix: string, value: unknown, parts: string[]) {
	if (isRecord(value)) {
		const entries = Object.entries(value);
		if (entries.length === 0) {
			parts.push(`${prefix}={}`);
			return;
		}
		for (const [key, child] of entries) {
			appendAttrParts(`${prefix}.${key}`, child, parts);
		}
		return;
	}

	parts.push(`${prefix}=${formatAttrValue(value)}`);
}

export function attrsText(attrs?: Record<string, unknown>): string {
	if (!attrs) return '';

	const parts: string[] = [];
	for (const [key, value] of Object.entries(attrs)) {
		appendAttrParts(key, value, parts);
	}
	return parts.join('  ');
}
