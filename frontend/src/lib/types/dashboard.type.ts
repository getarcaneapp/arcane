export type DashboardActionItemKind = 'stopped_containers' | 'image_updates' | 'actionable_vulnerabilities' | 'expiring_keys';

export type DashboardActionItemSeverity = 'warning' | 'critical';

export interface DashboardActionItem {
	kind: DashboardActionItemKind;
	count: number;
	severity: DashboardActionItemSeverity;
}

export interface DashboardActionItems {
	items: DashboardActionItem[];
}
