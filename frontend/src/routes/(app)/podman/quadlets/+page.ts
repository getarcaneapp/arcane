import { redirect } from '@sveltejs/kit';

import { queryKeys } from '#lib/query/query-keys.js';
import { podmanService } from '#lib/services/podman-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ url, parent }) => {
	const unit = url.searchParams.get('unit');
	if (!unit) return {};
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();
	const result = await tryCatch(
		queryClient.query({
			queryKey: queryKeys.podman.quadlets(envId),
			queryFn: () => podmanService.getQuadlets()
		})
	);
	const match = result.data?.find((quadlet) => quadlet.unitName === unit);
	if (match) redirect(307, `/podman/quadlets/${encodeURIComponent(match.name)}`);
	return {};
};
