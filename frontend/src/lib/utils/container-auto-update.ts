/**
 * Auto-update exclusion helpers shared by the container detail page, the
 * environment jobs tab and the updates table. The backend stores exclusions as
 * a CSV of container *names* in `autoUpdateExcludedContainers`, and the Docker
 * label `com.getarcaneapp.arcane.updater=false` overrides that setting.
 *
 * Keyed on labels + a resolved name rather than a container DTO: the detail
 * page has `ContainerDetailsDto.name` while the tables have
 * `ContainerSummaryDto.names[]`.
 */

const AUTO_UPDATE_LABEL = 'com.getarcaneapp.arcane.updater';
const DISABLED_LABEL_VALUES = ['false', '0', 'no', 'off'];

/** Strips the leading slashes Docker puts on container names. */
export function normalizeContainerName(name: string): string {
	return name.replace(/^\/+/, '');
}

/** True when the Docker label opts the container out of auto-updates. */
export function isAutoUpdateLabelDisabled(labels?: Record<string, string>): boolean {
	if (!labels) return false;
	const value = Object.entries(labels).find(([key]) => key.toLowerCase() === AUTO_UPDATE_LABEL)?.[1];
	return !!value && DISABLED_LABEL_VALUES.includes(value.trim().toLowerCase());
}

/** Parses an `autoUpdateExcludedContainers`-style CSV into normalized names. */
export function parseExcludedContainerSet(csv?: string): Set<string> {
	return new Set(
		(csv || '')
			.split(',')
			.map((entry) => normalizeContainerName(entry.trim()))
			.filter(Boolean)
	);
}

/** True when the container is excluded from auto-updates, by label or by setting. */
export function isAutoUpdateIgnored(name: string, labels: Record<string, string> | undefined, excludedCsv?: string): boolean {
	if (isAutoUpdateLabelDisabled(labels)) return true;
	const normalized = normalizeContainerName(name ?? '');
	if (!normalized) return false;
	return parseExcludedContainerSet(excludedCsv).has(normalized);
}
