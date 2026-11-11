import { m } from '#lib/paraglide/messages.js';
import { podmanSecretListQuery } from '#lib/query/podman.js';
import { environmentStatusStore } from '#lib/stores/environment-status.store.svelte.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { shownPodmanEnvironments } from '#lib/utils/podman.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready, environmentStatusStore.runtimeReady()]);
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
		queryClient.query(
			podmanSecretListQuery(
				scope,
				shownPodmanEnvironments(environmentStatusStore.podmanStatuses, 'podman-secrets:list'),
				requestOptions
			)
		)
	);
	if (secrets.error !== null) {
		throwPageLoadError(secrets.error, m.common_load_failed({ resource: m.podman_secrets_title() }));
	}

	return {
		secrets: secrets.data,
		requestOptions
	};
};
