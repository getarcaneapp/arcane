// --- Pagination, sort, filters ---

export type PaginationRequest = {
	page: number;
	limit: number;
};

export type SortRequest = {
	column: string;
	direction: 'asc' | 'desc';
};

export type FilterValue = string | number | boolean | (string | number | boolean)[];
export type FilterMap = Record<string, FilterValue>;

export type SearchPaginationSortRequest = {
	search?: string;
	pagination?: PaginationRequest;
	sort?: SortRequest;
	filters?: FilterMap;
	includeInternal?: boolean;
};

export type PaginationResponse = {
	totalPages: number;
	totalItems: number;
	currentPage: number;
	itemsPerPage: number;
	grandTotalItems?: number;
};

export type Paginated<T, C = unknown> = {
	data: T[];
	pagination: PaginationResponse;
	counts?: C;
};

// --- Events ---

export interface Event {
	id: string;
	type: string;
	severity: 'info' | 'warning' | 'error' | 'success';
	title: string;
	description?: string;
	resourceType?: string;
	resourceId?: string;
	resourceName?: string;
	userId?: string;
	username?: string;
	environmentId?: string;
	metadata?: Record<string, any>;
	timestamp: string;
	createdAt: string;
	updatedAt?: string;
}

// --- Variables (key/value form helper) ---

export interface Variable {
	key: string;
	value: string;
}

// --- System stats ---

export interface SystemStats {
	cpuUsage: number;
	memoryUsage: number;
	memoryTotal: number;
	diskUsage?: number;
	diskTotal?: number;
	cpuCount: number;
	architecture: string;
	platform: string;
	hostname?: string;
	gpuCount: number;
	gpus?: GPUStats[];
}

export interface GPUStats {
	name: string;
	index: number;
	memoryUsed: number;
	memoryTotal: number;
}

// --- File browser ---

export interface FileEntry {
	name: string;
	path: string;
	isDirectory: boolean;
	size: number;
	modTime: string;
	mode: string;
	isSymlink: boolean;
	linkTarget?: string;
}

export interface FileContentResponse {
	content: string;
	mimeType: string;
}

export interface BackupEntry {
	id: string;
	volumeName: string;
	size: number;
	createdAt: string;
}

// --- Customize search ---

export interface CustomizationMeta {
	key: string;
	label: string;
	type: string;
	keywords?: string[];
	description?: string;
}

export interface CustomizeCategory {
	id: string;
	title: string;
	description: string;
	icon: string;
	url: string;
	keywords: string[];
	customizations: CustomizationMeta[];
	matchingCustomizations?: CustomizationMeta[];
	relevanceScore?: number;
}

export interface CustomizeSearchResponse {
	results: CustomizeCategory[];
	query: string;
	count: number;
}

// --- Settings search ---

export interface SettingMeta {
	key: string;
	label: string;
	type: string;
	keywords?: string[];
	description?: string;
}

export interface SettingsCategory {
	id: string;
	title: string;
	description: string;
	icon: string;
	url: string;
	keywords: string[];
	settings: SettingMeta[];
	matchingSettings?: SettingMeta[];
	relevanceScore?: number;
}

export interface SettingsSearchResponse {
	results: SettingsCategory[];
	query: string;
	count: number;
}

// --- Dashboard ---

import type { ContainerStatusCounts, ContainerSummaryDto, ImageSummaryDto, ImageUsageCounts } from './docker';
import type { AppVersionInformation } from './settings';
import type { Environment } from './environment';

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
	versionInfo?: AppVersionInformation;
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
}
