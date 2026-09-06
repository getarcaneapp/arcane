export type JobRunStatus =
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
export type JobTargetOutcome = { id: string; status: JobRunStatus; message?: string; activityId?: string };
export type JobOutcome = { status: JobRunStatus; message?: string; activityId?: string; targets?: JobTargetOutcome[] };
export type JobAttempt = { number: number; startedAt: string; finishedAt?: string; outcome: JobOutcome };
export type JobRun = {
	id: string;
	jobId: string;
	environmentId: string;
	trigger: string;
	requestedBy?: string;
	status: JobRunStatus;
	createdAt: string;
	updatedAt: string;
	startedAt?: string;
	finishedAt?: string;
	nextAttempt?: string;
	attemptCount: number;
	outcome: JobOutcome;
	attempts?: JobAttempt[];
	remoteAccepted: boolean;
	remoteDeliveryAttempted: boolean;
	remoteOutcome?: JobOutcome;
	remoteSettled: boolean;
	lastConfirmedAt?: string;
};
export type JobRunList = { runs: JobRun[]; total: number; page: number; limit: number };
export type WorkerHealth = { status: string; lastError?: string; nextRetry?: string; updatedAt: string };
