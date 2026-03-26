import type { ContainerStatusCounts, ContainerSummaryDto } from './container.type';
import type { ImageSummaryDto, ImageUsageCounts } from './image.type';
import type { Paginated } from './pagination.type';

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

export interface DashboardSnapshotSettings {
	dockerPruneMode: 'all' | 'dangling';
}

export interface DashboardSnapshot {
	containers: Paginated<ContainerSummaryDto, ContainerStatusCounts>;
	images: Paginated<ImageSummaryDto>;
	imageUsageCounts: ImageUsageCounts;
	actionItems: DashboardActionItems;
	settings: DashboardSnapshotSettings;
}
