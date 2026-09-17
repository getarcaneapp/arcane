import { tryCatch } from '#lib/utils/try-catch.js';
import { version as currentVersion } from '$app/env';
import { apiClient } from './api-service';
import type { AppVersionInformation } from '#lib/types/settings.js';

function getCurrentVersion() {
	return currentVersion;
}

export function toAppVersionInformation(data: Partial<AppVersionInformation>): AppVersionInformation {
	return {
		currentVersion: data.currentVersion || getCurrentVersion(),
		currentTag: data.currentTag,
		currentDigest: data.currentDigest,
		displayVersion: data.displayVersion || data.currentVersion || getCurrentVersion(),
		revision: data.revision || 'unknown',
		shortRevision: data.shortRevision || data.revision?.slice(0, 8) || 'unknown',
		goVersion: data.goVersion || 'unknown',
		nodeVersion: data.nodeVersion || 'unknown',
		svelteKitVersion: data.svelteKitVersion || 'unknown',
		enabledFeatures: data.enabledFeatures ?? [],
		buildTime: data.buildTime,
		isSemverVersion: data.isSemverVersion || false,
		newestVersion: data.newestVersion,
		newestDigest: data.newestDigest,
		updateAvailable: data.updateAvailable || false,
		releaseUrl: data.releaseUrl,
		releaseNotes: data.releaseNotes,
		releasedAt: data.releasedAt
	};
}

async function getVersionInformation(): Promise<AppVersionInformation> {
	const operationResult = await tryCatch(
		(async () => {
			const res = await apiClient.get('/app-version', {
				timeout: 2000
			});
			return toAppVersionInformation(res.data as Partial<AppVersionInformation>);
		})()
	);
	return operationResult.error !== null ? toAppVersionInformation({}) : operationResult.data;
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
