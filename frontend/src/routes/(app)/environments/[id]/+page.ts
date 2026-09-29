import { featureStore } from '#lib/stores/features.store.svelte.js';
import { tryCatch } from '#lib/utils/try-catch.js';
import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
import { settingsService } from '#lib/services/settings-service.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { userHasPermission } from '#lib/utils/auth.js';
import { isEnvironmentOnline } from '#lib/utils/docker.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient, user } = await parent();

	const operationResult = await tryCatch(
		(async () => {
			const environment = await queryClient.query({
				queryKey: queryKeys.environments.detail(params.id),
				queryFn: () => environmentManagementService.get(params.id)
			});
			if (!environment.enabled || !isEnvironmentOnline(environment)) {
				await featureStore.markUnavailable(params.id);
				return { environment, settings: null, swarmActive: false };
			}
			await featureStore.load(params.id);
			// An unknown status only leaves the Swarm toggle unlocked; nothing else depends on it.
			const swarmActiveRequest = userHasPermission(user, 'swarm:read', params.id)
				? tryCatch(
						queryClient.query({
							queryKey: queryKeys.swarm.status(params.id),
							queryFn: () => swarmService.getSwarmStatus(params.id)
						})
					).then((result) => result.data?.enabled === true)
				: Promise.resolve(false);

			let settings = null;
			const operationResult = await tryCatch(
				(async () =>
					queryClient.query({
						queryKey: queryKeys.environments.settings(params.id),
						queryFn: () =>
							userHasPermission(user, 'settings:read', params.id)
								? settingsService.getSettingsForEnvironment(params.id)
								: settingsService.getPublicSettingsForEnvironment(params.id)
					}))()
			);
			if (operationResult.error !== null) {
			} else {
				settings = operationResult.data;
			}

			return {
				environment,
				settings,
				swarmActive: await swarmActiveRequest
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
