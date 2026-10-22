import type { ResolvedSettings } from '#lib/types/settings.js';

export function isDepotBuildAvailable(settings: ResolvedSettings | null | undefined): boolean {
	const projectId = (settings?.values.depotProjectId ?? '').trim();
	const token = (settings?.values.depotToken ?? '').trim();
	return Boolean(settings?.runtime.depotConfigured) || (Boolean(projectId) && Boolean(token));
}
