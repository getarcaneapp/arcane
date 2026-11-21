import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { containerService } from '#lib/services/container-service.js';
import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
import { eventService } from '#lib/services/event-service.js';
import { imageService } from '#lib/services/image-service.js';
import { jobScheduleService } from '#lib/services/job-schedule-service.js';
import { podmanService } from '#lib/services/podman-service.js';
import { projectService } from '#lib/services/project-service.js';
import { systemService } from '#lib/services/system-service.js';
import { vulnerabilityService } from '#lib/services/vulnerability-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { ContainerStatusCounts } from '#lib/types/container.js';
import type { Event } from '#lib/types/event.js';
import type { ImageUsageCounts } from '#lib/types/image.js';
import type { JobListResponse } from '#lib/types/jobschedule.js';
import type { PodmanStatus } from '#lib/types/podman.js';
import type { ProjectStatusCounts } from '#lib/types/project.js';
import type { SystemDiskUsage } from '#lib/types/system.js';
import type { VersionInfo } from '#lib/types/version.js';
import type { EnvironmentVulnerabilitySummary } from '#lib/types/vulnerability.js';
import {
	environmentScope,
	readAcrossEnvironments,
	sumAcrossEnvironments,
	type EnvironmentFailure
} from '#lib/utils/environments.js';
import { fetchRiskOverview } from '#lib/utils/vulnerability.js';

export const EMPTY_CONTAINER_COUNTS: ContainerStatusCounts = {
	runningContainers: 0,
	stoppedContainers: 0,
	totalContainers: 0,
	unhealthyContainers: 0,
	updatesAvailable: 0
};

export const EMPTY_IMAGE_COUNTS: ImageUsageCounts = {
	imagesInuse: 0,
	imagesUnused: 0,
	totalImages: 0,
	totalImageSize: 0
};

const EMPTY_DISK_USAGE: SystemDiskUsage = {
	unusedImagesBytes: 0,
	buildCacheBytes: 0,
	unusedVolumesBytes: 0,
	stoppedContainersBytes: 0
};

const RECENT_CONTAINER_EVENTS = { types: ['container.oom,container.die', 'container.start,container.restart'], limit: 50 };

export function dashboardContainerCountsQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.dashboard.summary('containers', environmentScope(environments), 'status-counts'),
		queryFn: () =>
			sumAcrossEnvironments(
				environments,
				(scope, config) => containerService.getContainerStatusCountsForEnvironment(scope, config),
				EMPTY_CONTAINER_COUNTS
			)
	});
}

export function dashboardProjectCountsQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.dashboard.summary('projects', environmentScope(environments), 'status-counts'),
		queryFn: () =>
			sumAcrossEnvironments<Pick<ProjectStatusCounts, 'updatesAvailable'>>(
				environments,
				(scope, config) => projectService.getProjectStatusCountsForEnvironment(scope, config),
				{ updatesAvailable: 0 }
			)
	});
}

export function dashboardImageCountsQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.dashboard.summary('images', environmentScope(environments), 'usage-counts'),
		queryFn: () =>
			sumAcrossEnvironments(
				environments,
				(scope, config) => imageService.getImageUsageCountsForEnvironment(scope, config),
				EMPTY_IMAGE_COUNTS
			)
	});
}

export function dashboardVulnerabilitySummaryQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.dashboard.summary('vulnerabilities', environmentScope(environments), 'summary'),
		queryFn: () =>
			sumAcrossEnvironments<Pick<EnvironmentVulnerabilitySummary, 'actionableCount'>>(
				environments,
				(scope, config) => vulnerabilityService.getEnvironmentSummary(scope, config),
				{ actionableCount: 0 }
			)
	});
}

export function dashboardPodmanStatusQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.dashboard.summary('podman', environmentScope(environments), 'status'),
		queryFn: () =>
			sumAcrossEnvironments<Pick<PodmanStatus, 'detected' | 'podCount'>>(
				environments,
				(scope, config) => podmanService.getStatus(scope, config),
				{ detected: false, podCount: 0 }
			)
	});
}

export function dashboardVersionsQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.dashboard.summary('environments', environmentScope(environments), 'versions'),
		queryFn: () =>
			sumAcrossEnvironments<Partial<VersionInfo>>(
				environments,
				(scope, config) => environmentManagementService.getVersion(scope, config),
				{}
			)
	});
}

export function dashboardRiskQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.dashboard.summary('vulnerabilities', environmentScope(environments), 'risk'),
		queryFn: () => fetchRiskOverview(environments)
	});
}

export function dashboardRecentEventsQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.dashboard.summary('events', environmentScope(environments), 'recent'),
		queryFn: async (): Promise<{
			events: Event[];
			failedEnvironments: EnvironmentFailure[];
			excludedEnvironments: EnvironmentRef[];
		}> => {
			if (environments.length === 0) return { events: [], failedEnvironments: [], excludedEnvironments: [] };
			const { value, failures, excluded } = await readAcrossEnvironments(environments, (scope, config) =>
				eventService.getRecentEnvironmentEvents(scope, RECENT_CONTAINER_EVENTS, config)
			);
			return { events: value, failedEnvironments: failures, excludedEnvironments: excluded };
		}
	});
}

export function dashboardJobsQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.dashboard.summary('environment-jobs', environmentScope(environments), 'list'),
		queryFn: () =>
			sumAcrossEnvironments<Pick<JobListResponse, 'jobs'>>(
				environments,
				(scope, config) => jobScheduleService.listJobs(scope, config),
				{ jobs: [] }
			)
	});
}

export function dashboardDiskUsageQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.dashboard.summary('system', environmentScope(environments), 'disk-usage'),
		queryFn: () =>
			sumAcrossEnvironments(environments, (scope, config) => systemService.getDiskUsage(scope, config), EMPTY_DISK_USAGE)
	});
}
