export type ActivityStatus = 'queued' | 'running' | 'success' | 'failed' | 'cancelled';

export type Activity = {
	id: string;
	type: string;
	status: ActivityStatus;
	resourceType?: string;
	resourceId?: string;
	resourceName?: string;
};
