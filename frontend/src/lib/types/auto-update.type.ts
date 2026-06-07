export type AutoUpdateResourceType = 'image' | 'container' | 'project';

export interface AutoUpdateCheck {
	type?: 'containers' | 'projects' | 'all';
	resourceIds?: string[];
	forceUpdate?: boolean;
	dryRun?: boolean;
	resourceType?: AutoUpdateResourceType;
}

export interface AutoUpdateResult {
	checked: number;
	updated: number;
	skipped: number;
	failed: number;
	items: AutoUpdateResourceResult[];
	duration: string;
}

export interface AutoUpdateResourceResult {
	resourceId: string;
	resourceName: string;
	resourceType: AutoUpdateResourceType;
	status: 'checked' | 'up_to_date' | 'update_available' | 'updated' | 'failed' | 'skipped';
	updateAvailable: boolean;
	updateApplied: boolean;
	oldImages?: Record<string, string>;
	newImages?: Record<string, string>;
	error?: string;
	details?: Record<string, any>;
}
