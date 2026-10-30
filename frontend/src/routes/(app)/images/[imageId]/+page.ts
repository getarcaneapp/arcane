import { error } from '@sveltejs/kit';

import { imageDetailQuery } from '#lib/query/images.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import type { ImageDetailSummaryDto } from '#lib/types/docker.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

type ImageDetailData = {
	envId: string;
	imageId: string;
	image: ImageDetailSummaryDto;
	error?: string;
};

export const load: PageLoad = async ({ params, parent, url }): Promise<ImageDetailData> => {
	const [{ queryClient }, envId] = await Promise.all([parent(), resolveDetailEnvironmentId(url)]);

	const operationResult = await tryCatch(
		Promise.all([queryClient.query(imageDetailQuery(envId, params.imageId)), featureStore.load(envId)])
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		console.error('Failed to load image:', err);
		if ('status' in err && err.status === 404) {
			throw err;
		}
		throw error(500, err.message || 'Failed to load image details');
	}

	const [image] = operationResult.data;
	if (!image) throw error(404, 'Image not found');

	return { envId, imageId: params.imageId, image };
};
