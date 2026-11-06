const AUTO_UPDATE_LABEL = 'com.getarcaneapp.arcane.updater';
/** Setting this label to a false-like value disables update checks and notifications. */
export const UPDATE_CHECK_LABEL = 'com.getarcaneapp.arcane.update-check';
/** Overrides the environment's auto-update delay, in hours, for one container. */
export const UPDATE_DELAY_LABEL = 'com.getarcaneapp.arcane.updater.delay';
const DISABLED_LABEL_VALUES = ['false', '0', 'no', 'off'];

export function isAutoUpdateLabelDisabled(labels?: Record<string, string>): boolean {
	if (!labels) return false;
	const value = Object.entries(labels).find(([key]) => key.toLowerCase() === AUTO_UPDATE_LABEL)?.[1];
	return !!value && DISABLED_LABEL_VALUES.includes(value.trim().toLowerCase());
}

export function parseExcludedContainerSet(csv?: string): Set<string> {
	return new Set(
		(csv || '')
			.split(',')
			.map((entry) => entry.trim())
			.filter(Boolean)
	);
}
