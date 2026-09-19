import type { ImageUpdateInfoDto } from '#lib/types/docker.js';
import { formatDateTimeShort } from '#lib/utils/formatting.js';

export function formatImageUpdateValue(updateInfo: ImageUpdateInfoDto | undefined, mode: 'current' | 'latest') {
	if (!updateInfo) return '-';

	const version = (mode === 'current' ? updateInfo.currentVersion : updateInfo.latestVersion)?.trim() ?? '';
	const digest = (mode === 'current' ? updateInfo.currentDigest : updateInfo.latestDigest)?.trim() ?? '';

	// A tag update is about the version that moved: the digest of the configured
	// tag can be unchanged, so showing it would hide the actual change.
	if (updateInfo.updateType === 'tag') {
		return version || digest || '-';
	}
	return digest || version || '-';
}

export function formatImageUpdateCheckedAt(value: string) {
	if (!value) return '-';
	return formatDateTimeShort(value) || '-';
}
