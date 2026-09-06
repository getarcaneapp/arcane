import { imageService } from '#lib/services/image-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { ImageDetailSummaryDto } from '#lib/types/docker.js';
import { parseImageRef } from '#lib/utils/docker.js';
import { queryKeys } from '#lib/query/query-keys.js';
import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';

type ImageDetailData = {
	image: ImageDetailSummaryDto;
	error?: string;
};

export const load: PageLoad = async ({ params, parent }): Promise<ImageDetailData> => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	const { imageId } = params;

	try {
		const image = await queryClient.fetchQuery({
			queryKey: queryKeys.images.detail(envId, imageId),
			queryFn: () => imageService.getImageForEnvironment(envId, imageId)
		});

		if (!image) {
			throw error(404, 'Image not found');
		}

		let repo = '<none>';
		let tag = '<none>';
		const rawTags = image.repoTags ?? image.RepoTags;
		if (rawTags && rawTags.length > 0 && rawTags[0] !== '<none>:<none>') {
			({ repo, tag } = parseImageRef(rawTags[0]));
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
