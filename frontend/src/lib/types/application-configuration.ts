export interface AppVersionInformation {
	currentVersion: string;
	currentTag?: string;
	currentDigest?: string;
	displayVersion: string;
	revision: string;
	shortRevision: string;
	goVersion: string;
	buildTime?: string;
	isSemverVersion: boolean;
	newestVersion?: string;
	newestDigest?: string;
	updateAvailable?: boolean;
	releaseUrl?: string;
	releaseNotes?: string;
}
