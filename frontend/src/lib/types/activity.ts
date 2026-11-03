export type ActivityStatus = 'queued' | 'running' | 'success' | 'failed' | 'cancelled';

export type ActivityType =
	| 'job_run'
	| 'image_pull'
	| 'image_build'
	| 'image_update_check'
	| 'image_patch'
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
	| 'container_edit'
	| 'container_delete'
	| 'container_kill'
	| 'container_pause'
	| 'container_unpause'
	| 'vulnerability_scan'
	| 'auto_update'
	| 'system_prune'
	| 'resource_action';

export type ActivityMessageLevel = 'info' | 'warning' | 'error' | 'success';

export interface Activity {
	startedAt: string;
	createdAt: string;
	batchId?: string;
	resourceType?: string;
	resourceId?: string;
	resourceName?: string;
	startedBy?: ActivityStartedBy;
	endedAt?: string;
	durationMs?: number;
	error?: string;
	metadata?: Record<string, unknown>;
	updatedAt?: string;
	environmentId: string;
	environmentName?: string;
	id: string;
	sourceEnvironmentId?: string;
	sourceEnvironmentName?: string;
	type: ActivityType;
	status: ActivityStatus;
	latestMessage?: string;
}

export interface ActivityStartedBy {
	userId?: string;
	username: string;
	displayName?: string;
}

export interface ActivityMessage {
	createdAt: string;
	payload?: Record<string, unknown>;
	id: string;
	activityId: string;
	level: ActivityMessageLevel;
	message: string;
}

export interface ActivityDetail {
	activity: Activity;
	messages: ActivityMessage[];
}

export interface ActivityStreamEvent {
	timestamp: string;
	activity?: Activity;
	message?: ActivityMessage;
	type: 'snapshot' | 'activity' | 'message' | 'heartbeat' | 'error';
	environmentId?: string;
	activityId?: string;
	error?: string;
	activities?: Activity[];
}

export interface ActivityClearHistoryResult {
	deleted: number;
}
