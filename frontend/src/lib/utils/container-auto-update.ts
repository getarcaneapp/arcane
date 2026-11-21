import { isContainerLabelDisabled, isContainerLabelEnabled } from './container-labels.js';

export const AUTO_UPDATE_LABEL = 'com.getarcaneapp.arcane.updater';
/** Setting this label to a false-like value disables update checks and notifications. */
export const UPDATE_CHECK_LABEL = 'com.getarcaneapp.arcane.update-check';
/** Overrides the environment's auto-update delay, in hours, for one container. */
export const UPDATE_DELAY_LABEL = 'com.getarcaneapp.arcane.updater.delay';

/** How the updater label decides a container's auto-update setting instead of its toggle. */
export type AutoUpdateLabelControl = 'disabled' | 'enabled';

/**
 * Reports whether the updater label decides auto-update for a container. updater=false always does,
 * updater=true only in opt-in mode, which older agents do not report.
 */
export function autoUpdateLabelControl(container: {
	labels?: Record<string, string>;
	autoUpdateOptIn?: boolean;
}): AutoUpdateLabelControl | undefined {
	if (isContainerLabelDisabled(container.labels, AUTO_UPDATE_LABEL)) return 'disabled';
	if (container.autoUpdateOptIn && isContainerLabelEnabled(container.labels, AUTO_UPDATE_LABEL)) return 'enabled';
	return undefined;
}

export function parseExcludedContainerSet(csv?: string): Set<string> {
	return new Set(
		(csv || '')
			.split(',')
			.map((entry) => entry.trim())
			.filter(Boolean)
	);
}
