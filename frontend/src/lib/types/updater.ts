export type AutoUpdateResourceType = 'image' | 'container' | 'project';

export interface UpdaterOptions {
	type?: AutoUpdateResourceType;
	resourceIds?: string[];
	forceUpdate?: boolean;
	dryRun?: boolean;
}

export interface UpdaterResourceResult {
	oldImages?: Record<string, string>;
	newImages?: Record<string, string>;
	details?: Record<string, unknown>;
	resourceId: string;
	resourceName?: string;
	resourceType: AutoUpdateResourceType;
	status: 'checked' | 'up_to_date' | 'update_available' | 'updated' | 'restarted' | 'failed' | 'skipped';
	error?: string;
	updateAvailable?: boolean;
	updateApplied?: boolean;
}

export interface UpdaterResult {
	activityId?: string;
	duration: string;
	items: UpdaterResourceResult[];
	checked: number;
	updated: number;
	restarted?: number;
	skipped: number;
	failed: number;
}
