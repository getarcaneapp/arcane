export interface ImageUpdateResponse {
	checkTime: string;
	imageUpdate?: ImageUpdateResponse;
	containerUpdates?: Record<string, ImageUpdateResponse>;
	activityId?: string;
	updateType: string;
	currentVersion: string;
	latestVersion?: string;
	currentDigest?: string;
	latestDigest?: string;
	currentCreated?: string;
	latestCreated?: string;
	error?: string;
	authMethod?: 'none' | 'anonymous' | 'credential' | 'unknown';
	authUsername?: string;
	authRegistry?: string;
	responseTimeMs: number;
	hasUpdate: boolean;
	usedCredential?: boolean;
}
