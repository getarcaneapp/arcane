export type JobSchedules = {
	environmentHealthInterval: string;
	eventCleanupInterval: string;
	analyticsHeartbeatInterval: string;
};

export type JobSchedulesUpdate = Partial<JobSchedules>;
