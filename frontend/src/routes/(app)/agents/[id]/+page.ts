import { environmentDetailQuery, environmentSettingsQuery } from '#lib/query/environments.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import { userHasPermission } from '#lib/utils/auth.js';
import { isEnvironmentOnline } from '#lib/utils/docker.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient, user } = await parent();

	const operationResult = await tryCatch(
		(async () => {
			const environment = await queryClient.query(environmentDetailQuery(params.id));
			if (!environment.enabled || !isEnvironmentOnline(environment)) {
				await featureStore.markUnavailable(params.id);
				return { environment, settings: null };
			}
			await featureStore.load(params.id);
			let settings = null;
			const operationResult = await tryCatch(
				(async () =>
					queryClient.query(
						environmentSettingsQuery(params.id, userHasPermission(user, 'environment-settings:read', params.id))
					))()
			);
			if (operationResult.error !== null) {
			} else {
				settings = operationResult.data;
			}

			return {
				environment,
				settings
			};
		})()
	);
	if (operationResult.error !== null) {
		const error = operationResult.error;

		console.error('Failed to load environment:', error);
		throw error;
	} else {
		return operationResult.data;
	}
};
