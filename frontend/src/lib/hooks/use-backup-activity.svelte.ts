import { tryCatch } from '#lib/utils/try-catch.js';
import { onMount } from 'svelte';
import { createQuery, useQueryClient } from '@tanstack/svelte-query';
import { queryKeys } from '#lib/query/query-keys.js';
import { activityService } from '#lib/services/activity-service.js';
import { clientStream } from '#lib/stores/client-stream.svelte.js';
import { STREAM_CHANNEL_ACTIVITIES } from '#lib/services/stream-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { Activity, ActivityStreamEvent } from '#lib/types/activity.type.js';

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
		const pollWithoutActivity = !!discover || !clientStream.streamConnected;
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
			refetchInterval: (query) => {
				if (pollWithoutActivity || query.state.data?.length) return 5000;
				return false;
			},
			refetchIntervalInBackground: true,
			staleTime: 0
		};
	});
	const activeIds = $derived(activeQuery.data ?? []);

	function refreshHistory() {
		void tryCatch(onChange()).then((result) => {
			if (result.error !== null) console.warn('Failed to refresh backup history', result.error);
		});
	}

	onMount(() => {
		const cache = queryClient.getQueryCache();
		const unsubscribeCache = cache.subscribe((event) => {
			if (event.type !== 'updated' || event.action.type !== 'success') return;
			if (event.query === cache.find({ queryKey, exact: true })) refreshHistory();
		});
		if (activeQuery.dataUpdatedAt) refreshHistory();

		const unsubscribeStream = clientStream.subscribe(STREAM_CHANNEL_ACTIVITIES, {
			onConnected() {
				void queryClient.invalidateQueries({ queryKey, exact: true });
			},
			onEvent(payload) {
				const event = payload as ActivityStreamEvent;
				const environmentId = getEnvironmentId();
				if ((event.environmentId || '0') !== environmentId) return;
				let activities: Activity[] = [];
				if (event.type === 'snapshot') activities = event.activities ?? [];
				else if (event.activity) activities = [event.activity];
				const relevant = activities.some(
					(activity) =>
						(activity.sourceEnvironmentId || activity.environmentId || event.environmentId || '0') === environmentId &&
						matches(activity)
				);
				const activeSnapshot = event.type === 'snapshot' && activeIds.length > 0;
				const activeMessage = event.type === 'message' && activeIds.includes(event.message?.activityId ?? event.activityId ?? '');
				if (!relevant && !activeSnapshot && !activeMessage) return;
				const key = queryKey;
				void queryClient.cancelQueries({ queryKey: key, exact: true });
				void queryClient.invalidateQueries({ queryKey: key, exact: true });
			}
		});
		return () => {
			unsubscribeCache();
			unsubscribeStream();
		};
	});

	return {
		get activeIds() {
			return activeIds;
		},
		accepted(activityId?: string) {
			// Cancel older discovery before adding the accepted operation to the cache.
			void queryClient.cancelQueries({ queryKey, exact: true });
			if (activityId) {
				queryClient.setQueryData<string[]>(queryKey, (ids = []) => {
					if (ids.includes(activityId)) return ids;
					return [...ids, activityId];
				});
			}
			void queryClient.invalidateQueries({ queryKey, exact: true });
		}
	};
}
