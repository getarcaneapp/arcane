import { queryKeys } from '#lib/query/query-keys.js';
import { settingsService } from '#lib/services/settings-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';

type ParentWithQueryClient = () => Promise<{
	queryClient: unknown;
	[key: string]: unknown;
}>;

type QueryClientLike = {
	fetchQuery: <T>(options: { queryKey: unknown; queryFn: () => Promise<T> }) => Promise<T>;
};

export async function loadMergedSettingsPage(parent: ParentWithQueryClient, errorContext: string) {
	const { queryClient } = await parent();
	const client = queryClient as QueryClientLike;
	const envId = await environmentStore.getCurrentEnvironmentId();

	try {
		const settings = await client.fetchQuery({
			queryKey: queryKeys.settings.byEnvironment(envId),
			queryFn: () => settingsService.getSettingsForEnvironmentMerged(envId)
		});
		return { settings };
	} catch (error) {
		console.error(`Failed to load ${errorContext}:`, error);
		throw error;
	}
}
