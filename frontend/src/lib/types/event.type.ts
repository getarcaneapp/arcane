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
	metadata?: Record<string, any>;
	timestamp: string;
	createdAt: string;
	updatedAt?: string;
}
