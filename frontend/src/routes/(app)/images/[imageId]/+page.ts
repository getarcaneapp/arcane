import { error } from '@sveltejs/kit';

import { m } from '#lib/paraglide/messages.js';
import { imageDetailQuery } from '#lib/query/images.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import type { ImageDetailSummary } from '#lib/types/image.js';
import { extractApiErrorStatus } from '#lib/utils/api.js';
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

	const imageLoad = await tryCatch(
		Promise.all([
			queryClient.ensureQueryData({ ...imageDetailQuery(envId, params.imageId), revalidateIfStale: true }),
			featureStore.load([envId])
		])
	);
	if (imageLoad.error !== null) {
		console.error('Failed to load image:', imageLoad.error);
		if (extractApiErrorStatus(imageLoad.error) === 404) {
			throw imageLoad.error;
		}
		throw error(500, imageLoad.error.message || m.failed_to_load_image_details());
	}

	const [image] = imageLoad.data;
	if (!image) throw error(404, 'Image not found');

	return { envId, imageId: params.imageId, image };
};
