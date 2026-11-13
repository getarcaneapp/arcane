import { m } from '#lib/paraglide/messages.js';
import type { ImageUpdateInfo } from '#lib/types/image.js';
import type { ImageUpdateResponse } from '#lib/types/imageupdate.js';
import { formatDateTimeShort } from '#lib/utils/formatting.js';

export function formatImageUpdateValue(
	updateInfo: ImageUpdateInfo | ImageUpdateResponse | undefined,
	mode: 'current' | 'latest'
) {
	if (!updateInfo) return '-';

	const version = (mode === 'current' ? updateInfo.currentVersion : updateInfo.latestVersion)?.trim() ?? '';
	const digest = (mode === 'current' ? updateInfo.currentDigest : updateInfo.latestDigest)?.trim() ?? '';

	// A tag update is about the version that moved: the digest of the configured
	// tag can be unchanged, so showing it would hide the actual change.
	const latestVersion = updateInfo.latestVersion?.trim() ?? '';
	if (updateInfo.updateType === 'tag' || (latestVersion && latestVersion !== updateInfo.currentVersion?.trim())) {
		return version || digest || '-';
	}
	return digest || version || '-';
}

export function blockedUpdateTitle(updateInfo: ImageUpdateInfo | undefined | null): string | null {
	if (!updateInfo?.hasUpdate || !updateInfo.blocked || updateInfo.error) return null;
	const reason = updateInfo.blockedReason?.trim();
	return reason ? `${m.update_blocked_by_vulnerability_limits()}: ${reason}` : m.update_blocked_by_vulnerability_limits();
}

export function formatImageUpdateCheckedAt(value: string) {
	if (!value) return '-';
	return formatDateTimeShort(value) || '-';
}
