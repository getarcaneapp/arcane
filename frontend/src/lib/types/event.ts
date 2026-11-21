export interface EnvironmentChanges {
	kinds: Record<string, number>;
	environmentId: string;
	epoch: string;
}

export interface Event {
	id: string;
	type: string;
	severity: 'info' | 'warning' | 'error' | 'success';
	title: string;
	description?: string;
	resourceType?: string;
	resourceId?: string;
	resourceName?: string;
	userId?: string;
	username?: string;
	environmentId?: string;
	metadata?: Record<string, unknown>;
	timestamp: string;
	createdAt: string;
	updatedAt?: string;
}

export interface EventSeverityCounts {
	total: number;
	info: number;
	success: number;
	warning: number;
	error: number;
}
