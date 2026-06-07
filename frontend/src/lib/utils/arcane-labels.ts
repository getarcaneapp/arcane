const ARCANE_ICON_LABELS = ['arcane.icon', 'com.getarcaneapp.arcane.icon'];

export function getArcaneIconUrlFromLabels(labels?: Record<string, string> | null): string | null {
	if (!labels) return null;

	for (const [key, value] of Object.entries(labels)) {
		const normalizedKey = key.trim().toLowerCase();
		if (ARCANE_ICON_LABELS.includes(normalizedKey)) {
			const trimmed = value?.trim();
			if (trimmed) return trimmed;
		}
	}

	return null;
}
