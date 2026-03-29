import { environmentManagementService } from '$lib/services/env-mgmt-service';
import { settingsService } from '$lib/services/settings-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import type { PageLoad } from './$types';

const environmentRequestOptions: SearchPaginationSortRequest = {
	pagination: {
		page: 1,
		limit: 1000
	},
	sort: {
		column: 'name',
		direction: 'asc'
	}
};

export const load: PageLoad = async ({ url, parent }) => {
	const { queryClient, versionInformation } = await parent();

	const environments = await queryClient.fetchQuery({
		queryKey: queryKeys.environments.list(environmentRequestOptions),
		queryFn: () => environmentManagementService.getEnvironments(environmentRequestOptions)
	});

	const requestedEnvironmentId = url.searchParams.get('environment');
	const availableEnvironments = environments.data;
	const selectedEnvironmentId =
		availableEnvironments.find((environment) => environment.id === requestedEnvironmentId)?.id ??
		availableEnvironments.find((environment) => environment.id === '0')?.id ??
		availableEnvironments[0]?.id ??
		null;

	if (!selectedEnvironmentId) {
		return {
			environments,
			selectedEnvironmentId: null,
			environment: null,
			settings: null,
			versionInformation
		};
	}

	const environment = await queryClient.fetchQuery({
		queryKey: queryKeys.environments.detail(selectedEnvironmentId),
		queryFn: () => environmentManagementService.get(selectedEnvironmentId)
	});

	let settings = null;
	try {
		settings = await queryClient.fetchQuery({
			queryKey: queryKeys.environments.settings(selectedEnvironmentId),
			queryFn: () => settingsService.getSettingsForEnvironment(selectedEnvironmentId)
		});
	} catch {}

	return {
		environments,
		selectedEnvironmentId,
		environment,
		settings,
		versionInformation
	};
};
