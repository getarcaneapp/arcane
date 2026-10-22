export type UpgradeJobResultStatus =
	| 'pending'
	| 'updating'
	| 'updated'
	| 'up_to_date'
	| 'triggered'
	| 'skipped_offline'
	| 'failed';

export type UpgradeJobStage = 'checking' | 'starting' | 'pulling' | 'upgrading' | 'reconnecting' | 'verifying';

export interface UpgradeJobResult {
	environmentId: string;
	environmentName: string;
	status: UpgradeJobResultStatus;
	fromVersion?: string;
	fromDigest?: string;
	toVersion?: string;
	stage?: UpgradeJobStage;
	stageStartedAt?: string;
	percent: number;
	logs?: string[];
	error?: string;
}

export type UpgradeJobStatus = 'pending_restart' | 'running' | 'completed' | 'failed';

export interface UpgradeJob {
	id: string;
	createdAt: string;
	updatedAt?: string;
	status: UpgradeJobStatus;
	userId: string;
	username: string;
	managerVersionAtStart: string;
	managerDigestAtStart: string;
	managerTargetVersion: string;
	results?: UpgradeJobResult[];
	error?: string;
	completedAt?: string;
}
