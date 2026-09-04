import { untrack } from 'svelte';
import { createQuery, useQueryClient } from '@tanstack/svelte-query';
import { queryKeys } from '#lib/query/query-keys';
import { activityService } from '#lib/services/activity-service';
import { activityStore } from '#lib/stores/activity.store.svelte';
import { environmentStore } from '#lib/stores/environment.store.svelte';
import type { Activity } from '#lib/types/activity.type';

export function useBackupActivity(
	getEnvironmentId: () => string,
	matches: (activity: Activity) => boolean,
	onChange: () => Promise<unknown>,
	getResourceKey: () => string = () => '',
	getDiscovery: () => ((environmentId: string) => Promise<string[]>) | undefined = () => undefined
) {
	const queryClient = useQueryClient();
	const queryKey = $derived(queryKeys.backupActivities.active(getEnvironmentId(), getResourceKey(), !!getDiscovery()));
	const activeQuery = createQuery(() => {
		const environmentId = getEnvironmentId();
		const discover = getDiscovery();
		return {
			queryKey,
			queryFn: async ({ signal }): Promise<string[]> => {
				await environmentStore.ready;
				signal.throwIfAborted();
				if (discover) return discover(environmentId);
				const activities: Activity[] = [];
				for (const status of ['queued', 'running']) {
					let page = 1;
					let totalPages = 1;
					do {
						const result = await activityService.getActivities(
							{ pagination: { page, limit: 100 }, filters: { status } },
							environmentId
						);
						signal.throwIfAborted();
						activities.push(...result.data);
						totalPages = result.pagination.totalPages;
						page++;
					} while (page <= totalPages);
				}
				return activities.filter(matches).map((activity) => activity.id);
			},
			refetchInterval: (query) => (discover || query.state.data?.length ? 5000 : false),
			refetchIntervalInBackground: true,
			staleTime: 0
		};
	});
	const activeIds = $derived(activeQuery.data ?? []);

	$effect(() => {
		if (!activeQuery.dataUpdatedAt) return;
		untrack(() => {
			void onChange().catch((error) => console.warn('Failed to refresh backup history', error));
		});
	});

	$effect(() => {
		const environmentId = getEnvironmentId();
		const key = queryKey;
		activityStore.connected;
		const events = activityStore.activities.filter(
			(activity) => (activity.sourceEnvironmentId || activity.environmentId) === environmentId && matches(activity)
		);
		// Include progress changes as well as terminal transitions.
		JSON.stringify(events);
		untrack(() => {
			void queryClient.cancelQueries({ queryKey: key, exact: true });
			void queryClient.invalidateQueries({ queryKey: key, exact: true });
		});
	});

	return {
		get activeIds() {
			return activeIds;
		},
		accepted(activityId?: string) {
			// Cancel older discovery before adding the accepted operation to the cache.
			void queryClient.cancelQueries({ queryKey, exact: true });
			if (activityId) {
				queryClient.setQueryData<string[]>(queryKey, (ids = []) => (ids.includes(activityId) ? ids : [...ids, activityId]));
			}
			void queryClient.invalidateQueries({ queryKey, exact: true });
		}
	};
}
