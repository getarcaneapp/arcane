import { m } from '#lib/paraglide/messages.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { podmanService } from '#lib/services/podman-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { fetchAcrossEnvironments } from '#lib/utils/environments.js';
import { shownPodmanEnvironments } from '#lib/utils/podman.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient, podmanStatuses } = await parent();
	const environments = shownPodmanEnvironments(podmanStatuses);
	const scope = environmentStore.shownKey;

	const requestOptions = resolveInitialTableRequest('arcane-podman-pods-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const pods = await tryCatch(
		queryClient.query({
			queryKey: queryKeys.podman.pods(scope, requestOptions),
			queryFn: () =>
				fetchAcrossEnvironments(environments, requestOptions, (id, request, config) => podmanService.getPods(id, request, config))
		})
	);
	if (pods.error !== null) {
		throwPageLoadError(pods.error, m.common_load_failed({ resource: m.podman_pods_title() }));
	}

	return {
		pods: pods.data,
		requestOptions
	};
};
