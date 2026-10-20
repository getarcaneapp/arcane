import { queryKeys } from '#lib/query/query-keys.js';
import { podmanService } from '#lib/services/podman-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { fetchAcrossEnvironments } from '#lib/utils/environments.js';
import { shownPodmanEnvironments } from '#lib/utils/podman.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient, podmanStatuses } = await parent();
	const environments = shownPodmanEnvironments(podmanStatuses);
	const scope = environmentStore.shownKey;

	const requestOptions = resolveInitialTableRequest('arcane-podman-secrets-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const secrets = await queryClient.query({
		queryKey: queryKeys.podman.secrets(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				podmanService.getSecrets(id, request, config)
			)
	});

	return {
		secrets,
		requestOptions
	};
};
