/** Opts a container in to auto-heal, which auto-heal opt-in mode requires. */
export const AUTO_HEAL_LABEL = 'com.getarcaneapp.arcane.auto-heal';
const DISABLED_LABEL_VALUES = ['false', '0', 'no', 'off'];
const ENABLED_LABEL_VALUES = ['true', '1', 'yes', 'on'];

// Mirrors the backend lookup: the exact key wins, then the smallest other spelling.
function containerLabelValue(labels: Record<string, string> | undefined, name: string): string | undefined {
	if (!labels) return undefined;
	const key =
		name in labels
			? name
			: Object.keys(labels)
					.filter((candidate) => candidate.trim().toLowerCase() === name)
					.sort()[0];
	return key === undefined ? undefined : labels[key]?.trim().toLowerCase();
}

/** Reports whether the label is set to a true-like value; names match case-insensitively. */
export function isContainerLabelEnabled(labels: Record<string, string> | undefined, name: string): boolean {
	const value = containerLabelValue(labels, name);
	return !!value && ENABLED_LABEL_VALUES.includes(value);
}

/** Reports whether the label is set to a false-like value; names match case-insensitively. */
export function isContainerLabelDisabled(labels: Record<string, string> | undefined, name: string): boolean {
	const value = containerLabelValue(labels, name);
	return !!value && DISABLED_LABEL_VALUES.includes(value);
}
