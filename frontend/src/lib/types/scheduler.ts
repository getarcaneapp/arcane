export type RunStatus =
	| 'queued'
	| 'waiting'
	| 'running'
	| 'retrying'
	| 'succeeded'
	| 'partial'
	| 'skipped'
	| 'failed'
	| 'needs_attention'
	| 'canceled';

export type TargetOutcome = {
	id: string;
	resourceType?: string;
	status: RunStatus;
	message?: string;
	activityId?: string;
};

export type StepOutcome = {
	name: string;
	status: RunStatus;
	total?: number;
	completed?: number;
	failed?: number;
	attempts?: number;
	message?: string;
	startedAt?: string;
	finishedAt?: string;
};

export type SchedulerOutcome = {
	status: RunStatus;
	message?: string;
	activityId?: string;
	targets?: TargetOutcome[];
	steps?: StepOutcome[];
};

export type SchedulerAttempt = { number: number; startedAt: string; finishedAt?: string; outcome: SchedulerOutcome };

export type SchedulerRun = {
	id: string;
	jobId: string;
	environmentId: string;
	trigger: string;
	requestedBy?: string;
	status: RunStatus;
	createdAt: string;
	updatedAt: string;
	startedAt?: string;
	finishedAt?: string;
	nextAttempt?: string;
	attemptCount: number;
	activityId?: string;
	activityEnvironmentId?: string;
	outcome: SchedulerOutcome;
	attempts?: SchedulerAttempt[];
	remoteAccepted: boolean;
	remoteDeliveryAttempted: boolean;
	remoteOutcome?: SchedulerOutcome;
	remoteSettled: boolean;
	lastConfirmedAt?: string;
};

export type SchedulerRunList = { runs: SchedulerRun[]; total: number; page: number; limit: number };

export type SchedulerWorkerHealth = { status: string; lastError?: string; nextRetry?: string; updatedAt: string };
