export const MAX_AVATAR_FILE_BYTES = 2 * 1024 * 1024;

export type AvatarUploadFileResult =
	| {
			ok: true;
			file: File;
	  }
	| {
			ok: false;
			reason: 'too_large';
	  };

export async function prepareAvatarUploadFile(
	url: string,
	maxSizeBytes: number,
	getFileFromUrl: (url: string, fileName: string) => Promise<File>
): Promise<AvatarUploadFileResult> {
	const file = await getFileFromUrl(url, 'avatar');

	if (file.size > maxSizeBytes) {
		return { ok: false, reason: 'too_large' };
	}

	return { ok: true, file };
}

export function avatarUploadLimitBytes(maxSizeMb: unknown): number {
	const parsed = Number(maxSizeMb);
	if (!Number.isFinite(parsed) || parsed <= 0) {
		return MAX_AVATAR_FILE_BYTES;
	}

	return Math.floor(parsed * 1024 * 1024);
}
