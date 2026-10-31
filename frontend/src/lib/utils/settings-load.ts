import { queryKeys } from '#lib/query/query-keys.js';
import { settingsService } from '#lib/services/settings-service.js';
import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
import { tryCatch } from '#lib/utils/try-catch.js';

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

	const operationResult = await tryCatch(
		(async () => {
			const settings = await client.query({
				queryKey: queryKeys.settings.byEnvironment(LOCAL_DOCKER_ENVIRONMENT_ID),
				queryFn: () => settingsService.getSettingsForEnvironmentMerged(LOCAL_DOCKER_ENVIRONMENT_ID)
			});
			return { settings: settings.values };
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
