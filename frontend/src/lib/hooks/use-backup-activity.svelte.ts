import { createQuery, useQueryClient } from '@tanstack/svelte-query';
import { onMount } from 'svelte';
import { toast } from 'svelte-sonner';

import { m } from '#lib/paraglide/messages.js';
import { backupActivityIdsQuery } from '#lib/query/backups.js';
import { STREAM_CHANNEL_ACTIVITIES } from '#lib/services/stream-service.js';
import { clientStream } from '#lib/stores/client-stream.svelte.js';
import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import type { Activity, ActivityStreamEvent } from '#lib/types/activity.js';
import { extractApiErrorMessage } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export function useBackupActivity(
	getEnvironmentId: () => string,
	matches: (activity: Activity) => boolean,
	onChange: () => Promise<unknown>,
	getResourceKey: () => string = () => '',
	getDiscovery: () => ((environmentId: string) => Promise<string[]>) | undefined = () => undefined
) {
	const queryClient = useQueryClient();
	const activeQueryOptions = $derived(backupActivityIdsQuery(getEnvironmentId(), getResourceKey(), matches, getDiscovery()));
	const queryKey = $derived(activeQueryOptions.queryKey);
	const activeQuery = createQuery(() => ({
		...activeQueryOptions,
		enabled: !!getDiscovery() || userStore.hasPermission('activities:list', getEnvironmentId()),
		staleTime: 0
	}));
	const activeIds = $derived(activeQuery.data ?? []);

	function refreshHistory() {
		void tryCatch(onChange()).then((result) => {
			if (result.error === null) return;
			toast.error(m.common_refresh_failed({ resource: m.backup_history() }), {
				description: extractApiErrorMessage(result.error)
			});
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
				if ((event.environmentId || LOCAL_DOCKER_ENVIRONMENT_ID) !== environmentId) return;
				let activities: Activity[] = [];
				if (event.type === 'snapshot') activities = event.activities ?? [];
				else if (event.activity) activities = [event.activity];
				const relevant = activities.some(
					(activity) =>
						(activity.sourceEnvironmentId || activity.environmentId || event.environmentId || LOCAL_DOCKER_ENVIRONMENT_ID) ===
							environmentId && matches(activity)
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
