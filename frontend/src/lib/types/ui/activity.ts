import type { Activity, ActivityStatus } from '../activity';

export interface ActivityBatchGroup {
	kind: 'batch';
	batchId: string;
	items: Activity[];
	total: number;
	done: number;
	failed: number;
	status: ActivityStatus;
}

export type ActivityGroup = { kind: 'single'; activity: Activity } | ActivityBatchGroup;

export interface ActivityEnvironmentFailure {
	environmentId: string;
	environmentName: string;
	message?: string;
}

export interface ActivityClearHistorySummary {
	deleted: number;
	succeeded: number;
	failed: ActivityEnvironmentFailure[];
}
