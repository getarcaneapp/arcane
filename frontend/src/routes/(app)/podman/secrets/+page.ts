import { m } from '#lib/paraglide/messages.js';
import { podmanSecretListQuery } from '#lib/query/podman.js';
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

	const secrets = await tryCatch(
		queryClient.query(podmanSecretListQuery(scope, shownPodmanEnvironments(podmanStatuses), requestOptions))
	);
	if (secrets.error !== null) {
		throwPageLoadError(secrets.error, m.common_load_failed({ resource: m.podman_secrets_title() }));
	}

	return {
		secrets: secrets.data,
		requestOptions
	};
};
