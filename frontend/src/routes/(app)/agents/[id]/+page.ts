import { environmentDetailQuery, environmentSettingsQuery } from '#lib/query/environments.js';
import { swarmStatusQuery } from '#lib/query/swarm.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import { userHasPermission } from '#lib/utils/auth.js';
import { isEnvironmentOnline } from '#lib/utils/docker.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient, user } = await parent();

	const environment = await tryCatch(queryClient.query(environmentDetailQuery(params.id)));
	if (environment.error !== null) {
		console.error('Failed to load environment:', environment.error);
		throw environment.error;
	}
	if (!environment.data.enabled || !isEnvironmentOnline(environment.data)) {
		await featureStore.markUnavailable(params.id);
		return { environment: environment.data, settings: null, swarmActive: false };
	}
	await featureStore.load([params.id]);
	// An unknown status only leaves the Swarm toggle unlocked; nothing else depends on it.
	let swarmActiveRequest = Promise.resolve(false);
	if (userHasPermission(user, 'swarm:read', params.id)) {
		swarmActiveRequest = tryCatch(queryClient.query(swarmStatusQuery([{ id: params.id, name: environment.data.name }]))).then(
			(result) => result.data?.enabled === true
		);
	}

	const settings = await tryCatch(
		queryClient.query(environmentSettingsQuery(params.id, userHasPermission(user, 'environment-settings:read', params.id)))
	);

	return {
		environment: environment.data,
		settings: settings.data,
		swarmActive: await swarmActiveRequest
	};
};
