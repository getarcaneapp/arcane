export type ActivityStatus = 'queued' | 'running' | 'success' | 'failed' | 'cancelled';

export type ActivityType =
	| 'image_pull'
	| 'image_build'
	| 'image_update_check'
	| 'project_pull'
	| 'project_build'
	| 'project_deploy'
	| 'project_redeploy'
	| 'project_down'
	| 'project_restart'
	| 'project_destroy'
	| 'container_start'
	| 'container_stop'
	| 'container_restart'
	| 'container_redeploy'
	| 'container_delete'
	| 'vulnerability_scan'
	| 'auto_update'
	| 'system_prune'
	| 'resource_action';

export type ActivityMessageLevel = 'info' | 'warning' | 'error' | 'success';

export type ActivityFilter = 'running' | 'failed' | 'completed';

export interface Activity {
	id: string;
	environmentId: string;
	sourceEnvironmentId?: string;
	sourceEnvironmentName?: string;
	type: ActivityType;
	status: ActivityStatus;
	resourceType?: string;
	resourceId?: string;
	resourceName?: string;
	progress?: number | null;
	step?: string;
	latestMessage?: string;
	startedBy?: ActivityStartedBy;
	startedAt: string;
	endedAt?: string;
	durationMs?: number;
	error?: string;
	metadata?: Record<string, unknown>;
	createdAt: string;
	updatedAt?: string;
}

export interface ActivityStartedBy {
	userId?: string;
	username: string;
	displayName?: string;
}

export interface ActivityMessage {
	id: string;
	activityId: string;
	level: ActivityMessageLevel;
	message: string;
	payload?: Record<string, unknown>;
	createdAt: string;
}

export interface ActivityDetail {
	activity: Activity;
	messages: ActivityMessage[];
}

export interface ActivityClearHistoryResult {
	deleted: number;
}

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

export interface ActivityStreamEvent {
	type: 'snapshot' | 'activity' | 'message' | 'heartbeat' | 'error';
	environmentId?: string;
	activityId?: string;
	activity?: Activity;
	activities?: Activity[];
	message?: ActivityMessage;
	error?: string;
	timestamp: string;
}
