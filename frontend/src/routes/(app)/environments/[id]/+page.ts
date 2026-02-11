import { environmentManagementService } from '$lib/services/env-mgmt-service';
import { settingsService } from '$lib/services/settings-service';
import { queryKeys } from '$lib/query/query-keys';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();

	try {
		const environment = await queryClient.fetchQuery({
			queryKey: queryKeys.environments.detail(params.id),
			queryFn: () => environmentManagementService.get(params.id)
		});

		let settings = null;
		try {
			settings = await queryClient.fetchQuery({
				queryKey: queryKeys.environments.settings(params.id),
				queryFn: () => settingsService.getSettingsForEnvironment(params.id)
			});
		} catch {}

		return {
			environment,
			settings
		};
	} catch (error) {
		console.error('Failed to load environment:', error);
		throw error;
	}
};
