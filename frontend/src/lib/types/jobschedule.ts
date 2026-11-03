import type { SchedulerRun, SchedulerWorkerHealth } from './scheduler';

export type JobScheduleConfig = {
	environmentHealthInterval: string;
	eventCleanupInterval: string;
	expiredSessionsCleanupInterval: string;
	autoUpdateInterval: string;
	dockerClientRefreshInterval: string;
	pollingInterval: string;
	scheduledPruneInterval: string;
	vulnerabilityScanInterval: string;
	imageAutoPatchInterval: string;
	autoHealInterval: string;
};

export type JobPrerequisite = {
	settingKey: string;
	label: string;
	isMet: boolean;
};

export type JobStatus = {
	currentRun?: SchedulerRun;
	lastRun?: SchedulerRun;
	lastSuccess?: string;
	lastError?: string;
	workerHealth?: SchedulerWorkerHealth;
	children?: JobStatus[];
	id: string;
	name: string;
	description: string;
	category: string;
	schedule: string;
	nextRun?: string;
	enabled: boolean;
	managerOnly: boolean;
	isContinuous: boolean;
	canRunManually: boolean;
	prerequisites: JobPrerequisite[];
	settingsKey?: string;
};

export type JobListResponse = {
	durableRuns: boolean;
	observedAt: string;
	timezone?: string;
	offline?: boolean;
	jobs: JobStatus[];
	isAgent: boolean;
};
