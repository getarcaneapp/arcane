import { error } from '@sveltejs/kit';

import { m } from '#lib/paraglide/messages.js';
import { containerDetailQuery } from '#lib/query/containers.js';
import { volumeDetailQuery } from '#lib/query/volumes.js';
import { extractApiErrorStatus } from '#lib/utils/api.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent, url }) => {
	const { queryClient } = await parent();
	const envId = await resolveDetailEnvironmentId(url);

	const { volumeName } = params;

	const volume = await tryCatch(queryClient.query(volumeDetailQuery(envId, volumeName)));
	if (volume.error !== null) {
		console.error('Failed to load volume:', volume.error);
		if (extractApiErrorStatus(volume.error) === 404) throw volume.error;
		throw error(500, volume.error.message || m.failed_to_load_volume_details());
	}

	const containersDetailed = await Promise.all(
		(volume.data.containers ?? []).map(async (id: string) => {
			const container = await tryCatch(queryClient.query(containerDetailQuery(envId, id)));
			if (container.error !== null) {
				return { id, name: id.substring(0, 12) };
			}
			const idVal = container.data?.id || id;
			return { id: idVal, name: container.data?.name || idVal.substring(0, 12) };
		})
	);

	return {
		envId,
		volume: volume.data,
		containersDetailed
	};
};
