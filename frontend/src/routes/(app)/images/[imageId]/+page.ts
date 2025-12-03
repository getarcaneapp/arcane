import { imageService } from '$lib/services/image-service.js';
import type { ImageDetailSummaryDto } from '$lib/types/image.type.js';
import { error } from '@sveltejs/kit';

type ImageDetailData = {
	image: ImageDetailSummaryDto;
	error?: string;
};

export const load = async ({ params }): Promise<ImageDetailData> => {
	const { imageId } = params;

	try {
		const image = await imageService.getImage(imageId);

		if (!image) {
			throw error(404, 'Image not found');
		}

		let repo = '<none>';
		let tag = '<none>';
		if (image.RepoTags && image.RepoTags.length > 0) {
			const repoTag = image.RepoTags[0];
			if (repoTag.includes(':')) {
				[repo, tag] = repoTag.split(':');
			} else {
				repo = repoTag;
				tag = 'latest';
			}
		}

		return {
			image: {
				...image,
				repo,
				tag
			}
		};
	} catch (err: any) {
		console.error('Failed to load image:', err);
		if (err.status === 404) {
			throw err;
		}
		throw error(500, err.message || 'Failed to load image details');
	}
};
