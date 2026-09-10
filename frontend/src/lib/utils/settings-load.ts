import { tryCatch } from '#lib/utils/try-catch.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { settingsService } from '#lib/services/settings-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';

type ParentWithQueryClient = () => Promise<{
	queryClient: unknown;
	[key: string]: unknown;
}>;

type QueryClientLike = {
	query: <T>(options: { queryKey: unknown; queryFn: () => Promise<T> }) => Promise<T>;
};

export async function loadMergedSettingsPage(parent: ParentWithQueryClient, errorContext: string) {
	const { queryClient } = await parent();
	const client = queryClient as QueryClientLike;
	const envId = await environmentStore.getCurrentEnvironmentId();

	const operationResult = await tryCatch(
		(async () => {
			const settings = await client.query({
				queryKey: queryKeys.settings.byEnvironment(envId),
				queryFn: () => settingsService.getSettingsForEnvironmentMerged(envId)
			});
			return { settings };
		})()
	);
	if (operationResult.error !== null) {
		const error = operationResult.error;
		console.error(`Failed to load ${errorContext}:`, error);
		throw error;
	} else {
		return operationResult.data;
	}
}
