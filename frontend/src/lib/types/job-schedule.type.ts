export type JobSchedules = {
	environmentHealthInterval: number;
	eventCleanupInterval: number;
	analyticsHeartbeatInterval: number;
};

export type JobSchedulesUpdate = Partial<JobSchedules>;
