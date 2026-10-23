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
	includeHidden?: boolean;
};

export type PaginationResponse = {
	totalPages: number;
	totalItems: number;
	currentPage: number;
	itemsPerPage: number;
	grandTotalItems?: number;
	hasNextPage?: boolean;
};

export type Paginated<T, C = unknown> = {
	data: T[];
	pagination: PaginationResponse;
	counts?: C;
};

export type CreateRequest<T, W extends keyof T, R extends W = never> = Pick<T, R> & Partial<Pick<T, Exclude<W, R>>>;

export type UpdateRequest<T, W extends keyof T> = Partial<Pick<T, W>>;

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

export interface EventSeverityCounts {
	total: number;
	info: number;
	success: number;
	warning: number;
	error: number;
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
	relativePath?: string;
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

import type { BackupDestination, BackupPolicyUpdate, BackupRun } from './backup';

export type VolumeBackupDestination = BackupDestination;

export interface CreateVolumeBackupRequest {
	destination?: VolumeBackupDestination;
	policyId?: string;
	s3DestinationId?: string;
}

export interface BackupEntry extends BackupRun {
	volumeName: string;
}

export interface VolumeBackupPolicy {
	id: string;
	volumeName: string;
	enabled: boolean;
	schedule: string;
	retentionCount: number;
	stopContainers: boolean;
	localEnabled: boolean;
	s3Enabled: boolean;
	s3DestinationId: string;
	s3DestinationName?: string;
	s3Available: boolean;
	s3Bucket: string;
	lastRun?: BackupEntry;
}

export type UpdateVolumeBackupPolicy = BackupPolicyUpdate;

export interface VolumeBackupPolicyCollection {
	policies: VolumeBackupPolicy[];
	s3Available: boolean;
}

export interface DiscoverVolumeBackupsResponse {
	count: number;
	errors?: string[];
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

import type { ContainerStatusCounts, ImageUsageCounts } from './docker';
import type { Environment } from './environment';
import type { AppVersionInformation } from './settings';

export interface DashboardEnvironmentOverview {
	environment: Environment;
	containers: ContainerStatusCounts;
	imageUsageCounts: ImageUsageCounts;
	updates: number;
	vulnerabilities: number;
	actionItemCount: number;
	versionInfo?: AppVersionInformation;
	podCount?: number;
	error?: string;
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
	totalVolumes: number;
	volumesInUse: number;
	volumesUnused: number;
	podmanEnvironments: number;
	totalPods: number;
}

export interface DashboardEnvironmentCardState {
	environment: Environment;
}

export type DashboardLiveStatsStatus = 'loading' | 'live' | 'stale' | 'unavailable' | 'denied';
