import { version as currentVersion } from '$app/environment';
import axios from 'axios';
import type { AppVersionInformation } from '$lib/types/application-configuration';

function getCurrentVersion() {
	return currentVersion;
}

async function getVersionInformation(): Promise<AppVersionInformation> {
	try {
		const res = await axios.get('/api/app-version', {
			timeout: 2000
		});
		const data = res.data as {
			currentVersion?: string;
			currentTag?: string;
			currentDigest?: string;
			displayVersion?: string;
			revision?: string;
			shortRevision?: string;
			goVersion?: string;
			buildTime?: string;
			isSemverVersion?: boolean;
			newestVersion?: string;
			newestDigest?: string;
			updateAvailable?: boolean;
			releaseUrl?: string;
		};

		return {
			currentVersion: data.currentVersion || getCurrentVersion(),
			currentTag: data.currentTag,
			currentDigest: data.currentDigest,
			displayVersion: data.displayVersion || data.currentVersion || getCurrentVersion(),
			revision: data.revision || 'unknown',
			shortRevision: data.shortRevision || data.revision?.slice(0, 8) || 'unknown',
			goVersion: data.goVersion || 'unknown',
			buildTime: data.buildTime,
			isSemverVersion: data.isSemverVersion || false,
			newestVersion: data.newestVersion,
			newestDigest: data.newestDigest,
			updateAvailable: data.updateAvailable || false,
			releaseUrl: data.releaseUrl
		};
	} catch (error) {
		// Fallback to basic version info if app-version endpoint fails
		return {
			currentVersion: getCurrentVersion(),
			displayVersion: getCurrentVersion(),
			revision: 'unknown',
			shortRevision: 'unknown',
			goVersion: 'unknown',
			isSemverVersion: false,
			updateAvailable: false
		};
	}
}

async function getNewestVersion(): Promise<string | undefined> {
	const info = await getVersionInformation();
	return info.newestVersion;
}

async function getReleaseUrl(): Promise<string | undefined> {
	const info = await getVersionInformation();
	return info.releaseUrl;
}

export default {
	getVersionInformation,
	getNewestVersion,
	getReleaseUrl,
	getCurrentVersion
};
