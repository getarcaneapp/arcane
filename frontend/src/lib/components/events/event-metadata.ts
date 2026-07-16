export type MetadataEntry = {
	key: string;
	value: string;
};

export function stringifyForDisplay(value: unknown): string {
	if (value === null || value === undefined) {
		return '';
	}
	if (typeof value === 'string') {
		return value;
	}
	if (typeof value === 'number' || typeof value === 'boolean') {
		return String(value);
	}
	try {
		return JSON.stringify(value, null, 2);
	} catch {
		return String(value);
	}
}

export function flattenMetadata(value: unknown, prefix = ''): MetadataEntry[] {
	if (Array.isArray(value)) {
		if (value.length === 0) {
			return prefix ? [{ key: prefix, value: '[]' }] : [];
		}
		return value.flatMap((item, index) => {
			const key = prefix ? `${prefix}[${index}]` : `[${index}]`;
			return flattenMetadata(item, key);
		});
	}

	if (value && typeof value === 'object') {
		const objectValue = value as Record<string, unknown>;
		const keys = Object.keys(objectValue).sort();
		if (keys.length === 0) {
			return prefix ? [{ key: prefix, value: '{}' }] : [];
		}
		return keys.flatMap((key) => {
			const nextPrefix = prefix ? `${prefix}.${key}` : key;
			return flattenMetadata(objectValue[key], nextPrefix);
		});
	}

	if (!prefix) {
		return [];
	}
	return [{ key: prefix, value: stringifyForDisplay(value) }];
}
