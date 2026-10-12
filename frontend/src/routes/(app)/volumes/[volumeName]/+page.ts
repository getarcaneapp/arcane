import { error } from '@sveltejs/kit';

import { queryKeys } from '#lib/query/query-keys.js';
import { containerService } from '#lib/services/container-service.js';
import { volumeService } from '#lib/services/volume-service.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent, url }) => {
	const { queryClient } = await parent();
	const envId = await resolveDetailEnvironmentId(url);

	const { volumeName } = params;

	const operationResult = await tryCatch(
		(async () => {
			const volume = await queryClient.query({
				queryKey: queryKeys.volumes.detail(envId, volumeName),
				queryFn: () => volumeService.getVolumeForEnvironment(envId, volumeName)
			});

			let containersDetailed: { id: string; name: string }[] = [];
			if (volume.containers && volume.containers.length > 0) {
				containersDetailed = await Promise.all(
					volume.containers.map(async (id: string) => {
						const operationResult = await tryCatch(
							(async () => {
								const c = await queryClient.query({
									queryKey: queryKeys.containers.detail(envId, id),
									queryFn: () => containerService.getContainerForEnvironment(envId, id)
								});
								const idVal = c?.id || id;
								const nameVal = c?.name || idVal.substring(0, 12);
								return { id: idVal, name: nameVal };
							})()
						);
						if (operationResult.error !== null) {
							return { id, name: id.substring(0, 12) };
						} else {
							return operationResult.data;
						}
					})
				);
			}

			return {
				envId,
				volume,
				containersDetailed
			};
		})()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		console.error('Failed to load volume:', err);
		if ('status' in err && err.status === 404) throw err;
		throw error(500, err.message || 'Failed to load volume details');
	} else {
		return operationResult.data;
	}
};
