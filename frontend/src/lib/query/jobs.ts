import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { jobScheduleService } from '#lib/services/job-schedule-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { JobListResponse, JobScheduleConfig } from '#lib/types/jobschedule.js';
import { environmentScope, readEachEnvironment } from '#lib/utils/environments.js';

const EMPTY_SCHEDULES: JobScheduleConfig = {
	environmentHealthInterval: '',
	eventCleanupInterval: '',
	expiredSessionsCleanupInterval: '',
	autoUpdateInterval: '',
	dockerClientRefreshInterval: '',
	pollingInterval: '',
	scheduledPruneInterval: '',
	vulnerabilityScanInterval: '',
	imageAutoPatchInterval: '',
	autoHealInterval: ''
};

export function environmentJobsQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.jobs.list(environmentId),
		queryFn: ({ signal }) => jobScheduleService.listJobs(environmentId, { signal, suppressAccessDeniedToast: true })
	});
}

export function jobRunsQuery(environmentId: string, jobId: string, page: number) {
	return queryOptions({
		queryKey: queryKeys.jobs.runs(environmentId, jobId, page),
		queryFn: () => jobScheduleService.listRuns(jobId, environmentId, page)
	});
}

export function jobRunQuery(environmentId: string, jobId: string, runId: string | undefined) {
	return queryOptions({
		queryKey: queryKeys.jobs.run(environmentId, jobId, runId),
		queryFn: () => jobScheduleService.getRun(jobId, runId ?? '', environmentId)
	});
}

export function jobSchedulesQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.jobs.schedules(environmentScope(environments)),
		queryFn: () =>
			readEachEnvironment(environments, (scope, config) => jobScheduleService.getJobSchedules(scope, config), EMPTY_SCHEDULES)
	});
}

export function jobAutomationsQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.jobs.automations(environmentScope(environments)),
		queryFn: ({ signal }) =>
			readEachEnvironment<Pick<JobListResponse, 'timezone'>>(
				environments,
				(scope, config) => jobScheduleService.listJobs(scope, { ...config, signal }),
				{}
			)
	});
}
