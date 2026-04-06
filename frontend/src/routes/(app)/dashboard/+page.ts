import { dashboardService } from '$lib/services/dashboard-service';
import { settingsService } from '$lib/services/settings-service';
import { queryKeys } from '$lib/query/query-keys';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { throwPageLoadError } from '$lib/utils/page-load-error.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, url }) => {
	const { queryClient } = await parent();
	const debugAllGood = url.searchParams.get('debugAllGood') === 'true';
	const requestedView = url.searchParams.get('view');
	const view = requestedView === 'current' ? 'current' : 'all';

	try {
		if (view === 'all') {
			return {
				view,
				dashboard: null,
				debugAllGood
			};
		}

		const envId = await environmentStore.getCurrentEnvironmentId();
		const [dashboard, settings] = await Promise.all([
			queryClient.fetchQuery({
				queryKey: queryKeys.dashboard.snapshot(envId, debugAllGood),
				queryFn: () => dashboardService.getDashboardForEnvironment(envId, { debugAllGood })
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.settings.byEnvironment(envId),
				queryFn: () => settingsService.getSettingsForEnvironmentMerged(envId)
			})
		]);

		return {
			view,
			dashboard,
			settings,
			debugAllGood
		};
	} catch (err) {
		throwPageLoadError(err, 'Failed to load dashboard data');
	}
};
