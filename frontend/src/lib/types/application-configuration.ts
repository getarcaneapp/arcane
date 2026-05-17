export interface AppVersionInformation {
	currentVersion: string;
	currentTag?: string;
	currentDigest?: string;
	displayVersion: string;
	revision: string;
	shortRevision: string;
	goVersion: string;
	enabledFeatures?: string[];
	buildTime?: string;
	isSemverVersion: boolean;
	newestVersion?: string;
	newestDigest?: string;
	updateAvailable?: boolean;
	manualUpdateRequired?: boolean;
	manualUpdateMessage?: string;
	releaseUrl?: string;
	releaseNotes?: string;
	releasedAt?: string;
}
