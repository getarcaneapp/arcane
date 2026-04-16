import type { ContainerStatusCounts, ContainerSummaryDto } from './container.type';
import type { Environment } from './environment.type';
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

export interface DashboardSnapshotSettings {}

export interface DashboardSnapshot {
	containers: Paginated<ContainerSummaryDto, ContainerStatusCounts>;
	images: Paginated<ImageSummaryDto>;
	imageUsageCounts: ImageUsageCounts;
	actionItems: DashboardActionItems;
	settings: DashboardSnapshotSettings;
}

export type EnvironmentDashboardSnapshotState = 'ready' | 'skipped' | 'error';

export interface DashboardEnvironmentOverview {
	environment: Environment;
	containers: ContainerStatusCounts;
	imageUsageCounts: ImageUsageCounts;
	actionItems: DashboardActionItems;
	settings: DashboardSnapshotSettings;
	snapshotState: EnvironmentDashboardSnapshotState;
	snapshotError?: string;
}

export interface DashboardEnvironmentsSummary {
	totalEnvironments: number;
	onlineEnvironments: number;
	standbyEnvironments: number;
	offlineEnvironments: number;
	pendingEnvironments: number;
	errorEnvironments: number;
	disabledEnvironments: number;
	containers: ContainerStatusCounts;
	imageUsageCounts: ImageUsageCounts;
	environmentsWithActionItems: number;
}

export interface DashboardEnvironmentsOverview {
	summary: DashboardEnvironmentsSummary;
	environments: DashboardEnvironmentOverview[];
}

export interface DashboardOverviewSummary {
	totalEnvironments: number;
	reachableEnvironments: number;
	unavailableEnvironments: number;
	disabledEnvironments: number;
	totalContainers: number;
	runningContainers: number;
	stoppedContainers: number;
	totalImages: number;
	imagesInUse: number;
	imagesUnused: number;
	totalImageSize: number;
}

export interface DashboardEnvironmentCardState {
	environment: Environment;
	loadPromise: Promise<DashboardEnvironmentOverview> | null;
}
