import type { ImageUpdateInfoDto } from '$lib/types/docker';
import { format } from 'date-fns';

export function formatImageUpdateValue(updateInfo: ImageUpdateInfoDto | undefined, mode: 'current' | 'latest') {
	if (!updateInfo) return '-';

	const digest = mode === 'current' ? updateInfo.currentDigest : updateInfo.latestDigest;
	if (digest?.trim()) return digest.trim();

	const version = mode === 'current' ? updateInfo.currentVersion : updateInfo.latestVersion;
	if (version?.trim()) return version.trim();

	return '-';
}

export function formatImageUpdateCheckedAt(value: string) {
	if (!value) return '-';
	const parsed = new Date(value);
	if (Number.isNaN(parsed.getTime())) return '-';
	return format(parsed, 'PP p');
}
