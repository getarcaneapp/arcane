export type JobSchedules = {
	environmentHealthInterval: string;
	eventCleanupInterval: string;
	analyticsHeartbeatInterval: string;
};

export type JobSchedulesUpdate = Partial<JobSchedules>;

export type JobPrerequisite = {
	settingKey: string;
	label: string;
	isMet: boolean;
	settingsUrl?: string;
};

export type JobStatus = {
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
	jobs: JobStatus[];
	isAgent: boolean;
};

export type JobRunResponse = {
	success: boolean;
	message: string;
};
