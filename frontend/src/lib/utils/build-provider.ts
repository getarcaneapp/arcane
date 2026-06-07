type DepotSettings = {
	depotConfigured?: boolean;
	depotProjectId?: unknown;
	depotToken?: unknown;
};

export function isDepotBuildAvailable(settings: DepotSettings | null | undefined): boolean {
	const projectId = String(settings?.depotProjectId ?? '').trim();
	const token = String(settings?.depotToken ?? '').trim();
	return Boolean(settings?.depotConfigured) || (Boolean(projectId) && Boolean(token));
}
