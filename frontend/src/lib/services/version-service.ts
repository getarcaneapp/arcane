import { version as currentVersion } from '$app/env';

import type { VersionInfo } from '#lib/types/version.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import { apiClient } from './api-service';

function getCurrentVersion() {
	return currentVersion;
}

export function toAppVersionInformation(data: Partial<VersionInfo>): VersionInfo {
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
		isStableVersion: data.isStableVersion || false,
		newestVersion: data.newestVersion,
		newestDigest: data.newestDigest,
		updateAvailable: data.updateAvailable || false,
		releaseUrl: data.releaseUrl,
		releaseNotes: data.releaseNotes,
		releasedAt: data.releasedAt
	};
}

async function getVersionInformation(): Promise<VersionInfo> {
	const response = await tryCatch(apiClient.get<Partial<VersionInfo>>('/app/version', { timeout: 2000 }));
	if (response.error !== null) return toAppVersionInformation({});
	return toAppVersionInformation(response.data.data);
}

async function getNewestVersion(): Promise<string | undefined> {
	const info = await getVersionInformation();
	return info.newestVersion;
}

async function getReleaseUrl(): Promise<string | undefined> {
	const info = await getVersionInformation();
	return info.releaseUrl;
}

async function getLatestGitHubRelease(): Promise<{ tag: string; body: string; publishedAt: string; url: string }> {
	const res = await fetch('https://api.github.com/repos/getarcaneapp/arcane/releases/latest', {
		headers: { Accept: 'application/vnd.github+json' }
	});
	if (!res.ok) throw new Error(`GitHub returned ${res.status}`);
	const data: { tag_name?: string; body?: string; published_at?: string; html_url?: string } = await res.json();
	return {
		tag: data.tag_name ?? '',
		body: data.body ?? '',
		publishedAt: data.published_at ?? '',
		url: data.html_url ?? ''
	};
}

export default {
	getVersionInformation,
	getLatestGitHubRelease,
	getNewestVersion,
	getReleaseUrl,
	getCurrentVersion
};
