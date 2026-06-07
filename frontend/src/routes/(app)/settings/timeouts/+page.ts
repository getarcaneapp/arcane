import type { PageLoad } from './$types';
import { settingsService } from '$lib/services/settings-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { queryKeys } from '$lib/query/query-keys';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	try {
		const settings = await queryClient.fetchQuery({
			queryKey: queryKeys.settings.byEnvironment(envId),
			queryFn: () => settingsService.getSettingsForEnvironmentMerged(envId)
		});
		return { settings };
	} catch (error) {
		console.error('Failed to load timeout settings:', error);
		throw error;
	}
};
