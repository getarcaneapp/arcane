export type UpdateAllEnvironmentStatus =
	| 'pending'
	| 'updating'
	| 'updated'
	| 'up_to_date'
	| 'triggered'
	| 'skipped_offline'
	| 'failed';

export type UpdateAllStage = 'checking' | 'starting' | 'reconnecting' | 'verifying';

export interface UpdateAllEnvironmentResult {
	environmentId: string;
	environmentName: string;
	status: UpdateAllEnvironmentStatus;
	stage?: UpdateAllStage;
	stageStartedAt?: string;
	fromVersion?: string;
	toVersion?: string;
	error?: string;
}

export type UpdateAllJobStatus = 'pending_restart' | 'running' | 'completed' | 'failed';

export interface UpdateAllJob {
	id: string;
	status: UpdateAllJobStatus;
	results?: UpdateAllEnvironmentResult[];
	error?: string;
	createdAt: string;
	completedAt?: string;
}
