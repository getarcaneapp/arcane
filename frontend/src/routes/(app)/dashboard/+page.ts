import { dashboardService } from '$lib/services/dashboard-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { queryKeys } from '$lib/query/query-keys';
import { throwPageLoadError } from '$lib/utils/page-load-error.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, url }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();
	const debugAllGood = url.searchParams.get('debugAllGood') === 'true';

	try {
		const dashboard = await queryClient.fetchQuery({
			queryKey: queryKeys.dashboard.snapshot(envId, debugAllGood),
			queryFn: () => dashboardService.getDashboardForEnvironment(envId, { debugAllGood })
		});

		return {
			dashboard,
			debugAllGood
		};
	} catch (err) {
		throwPageLoadError(err, 'Failed to load dashboard data');
	}
};
