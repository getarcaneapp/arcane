export type PatchMode = 'update-all' | 'report';

export type PatchStatus = 'patching' | 'completed' | 'failed';

export interface PatchOptions {
	suffix?: string;
	patchedTag?: string;
	scanId?: string;
	timeoutSeconds?: number;
	ignoreErrors?: boolean;
}

export interface PatchScanSummary {
	scanTime: string;
	status: 'pending' | 'scanning' | 'completed' | 'failed';
	fixableCount: number;
	totalCount: number;
}

export interface PatchTarget {
	scanTime: string;
	lastPatch?: PatchRecord;
	lastPatchScan?: PatchScanSummary;
	imageId: string;
	imageRef: string;
	fixableCount: number;
	totalCount: number;
	localOnly?: boolean;
}

export interface PatchRecord {
	createdAt: string;
	packagesUpdated?: number;
	error?: string;
	activityId?: string;
	durationMs?: number;
	updatedAt?: string;
	id: string;
	environmentId: string;
	originalImageId: string;
	originalRef: string;
	originalDigest?: string;
	patchedRef: string;
	mode: PatchMode;
	status: PatchStatus;
}
