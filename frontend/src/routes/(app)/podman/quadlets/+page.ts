import { redirect } from '@sveltejs/kit';

import { m } from '#lib/paraglide/messages.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { podmanService } from '#lib/services/podman-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { environmentHref, fetchAcrossEnvironments, resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { shownPodmanEnvironments } from '#lib/utils/podman.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ url, parent }) => {
	const { queryClient, podmanStatuses } = await parent();
	const environments = shownPodmanEnvironments(podmanStatuses, (status) => status.quadletsSupported);
	const scope = environmentStore.shownKey;

	const unit = url.searchParams.get('unit');
	if (unit) {
		const envId = await resolveDetailEnvironmentId(url);
		const match = await tryCatch(podmanService.getQuadlets(envId, { search: unit, pagination: { page: 1, limit: -1 } }));
		const quadlet = match.data?.data.find((item) => item.unitName === unit);
		if (quadlet) redirect(307, environmentHref(`/podman/quadlets/${encodeURIComponent(quadlet.name)}`, envId));
	}

	const requestOptions = resolveInitialTableRequest('arcane-podman-quadlets-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const quadlets = await tryCatch(
		queryClient.query({
			queryKey: queryKeys.podman.quadlets(scope, requestOptions),
			queryFn: () =>
				fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
					podmanService.getQuadlets(id, request, config)
				)
		})
	);
	if (quadlets.error !== null) {
		throwPageLoadError(quadlets.error, m.common_load_failed({ resource: m.podman_quadlets_title() }));
	}

	return {
		quadlets: quadlets.data,
		requestOptions
	};
};
