import type { ContainerStatusCounts } from '../container';
import type { Environment } from '../environment';
import type { ImageUsageCounts } from '../image';
import type { VersionInfo } from '../version';

export interface DashboardEnvironmentOverview {
	environment: Environment;
	containers: ContainerStatusCounts;
	imageUsageCounts: ImageUsageCounts;
	updates: number;
	vulnerabilities: number;
	actionItemCount: number;
	versionInfo?: VersionInfo;
	podCount?: number;
	error?: string;
}

export interface DashboardEnvironmentCardState {
	environment: Environment;
}

export type DashboardLiveStatsStatus = 'loading' | 'live' | 'stale' | 'unavailable' | 'denied';
