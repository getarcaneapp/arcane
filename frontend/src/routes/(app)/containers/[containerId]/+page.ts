import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';
import { containerService } from '$lib/services/container-service';
import { settingsService } from '$lib/services/settings-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { queryKeys } from '$lib/query/query-keys';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();
	const containerId = params.containerId;

	try {
		const [container, settings] = await Promise.all([
			queryClient.fetchQuery({
				queryKey: queryKeys.containers.detail(envId, containerId),
				queryFn: () => containerService.getContainerForEnvironment(envId, containerId)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.settings.byEnvironment(envId),
				queryFn: () => settingsService.getSettingsForEnvironmentMerged(envId)
			})
		]);

		if (!container) {
			throw error(404, 'Container not found');
		}

		return {
			container,
			settings
		};
	} catch (err: any) {
		console.error('Failed to load container:', err);
		if (err.status === 404) {
			throw err;
		}
		throw error(500, err.message || 'Failed to load container details');
	}
};
