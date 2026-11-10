import { error } from '@sveltejs/kit';

import { m } from '#lib/paraglide/messages.js';
import { imageDetailQuery } from '#lib/query/images.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import type { ImageDetailSummary } from '#lib/types/image.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

type ImageDetailData = {
	envId: string;
	imageId: string;
	image: ImageDetailSummary & { repo: string; tag: string };
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
		throw error(500, err.message || m.failed_to_load_image_details());
	}

	const [image] = operationResult.data;
	if (!image) throw error(404, 'Image not found');

	return { envId, imageId: params.imageId, image };
};
