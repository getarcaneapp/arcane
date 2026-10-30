import { m } from '#lib/paraglide/messages.js';
import { podmanPodListQuery } from '#lib/query/podman.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { shownPodmanEnvironments } from '#lib/utils/podman.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient, podmanStatuses }] = await Promise.all([parent(), environmentStore.ready]);
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
		queryClient.query(podmanPodListQuery(scope, shownPodmanEnvironments(podmanStatuses), requestOptions))
	);
	if (pods.error !== null) {
		throwPageLoadError(pods.error, m.common_load_failed({ resource: m.podman_pods_title() }));
	}

	return {
		pods: pods.data,
		requestOptions
	};
};
