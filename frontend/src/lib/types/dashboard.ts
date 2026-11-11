import type { ContainerStatusCounts, ContainerSummary } from './container';
import type { ImageUsageCounts } from './image';
import type { ProjectStatusCounts } from './project';
import type { SystemDiskUsage } from './system';
import type { VersionInfo } from './version';
import type { EnvironmentVulnerabilitySummary, RiskOverview } from './vulnerability';

export type DashboardSection<T> = {
	data: T;
	error?: string;
};

export interface DashboardSummary {
	containers?: DashboardSection<ContainerStatusCounts>;
	projects?: DashboardSection<ProjectStatusCounts>;
	images?: DashboardSection<ImageUsageCounts>;
	vulnerabilities?: DashboardSection<EnvironmentVulnerabilitySummary | null>;
	risk?: DashboardSection<RiskOverview | null>;
	diskUsage?: DashboardSection<SystemDiskUsage>;
	topMemory?: DashboardSection<ContainerSummary[]>;
	version?: DashboardSection<VersionInfo | null>;
}

export type DashboardSectionId = keyof DashboardSummary;
