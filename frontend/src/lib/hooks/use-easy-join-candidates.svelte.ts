import { createQuery } from '@tanstack/svelte-query';
import { fromStore } from 'svelte/store';
import { queryKeys } from '#lib/query/query-keys.js';
import { userHasPermission } from '#lib/utils/auth.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import userStore from '#lib/stores/user-store.js';
import type { SwarmJoinCandidate } from '#lib/types/swarm.js';

export function useEasyJoinCandidates() {
	const user = fromStore(userStore);
	const managerEnvironmentId = $derived(environmentStore.selected?.id ?? null);
	const canDiscover = $derived(
		managerEnvironmentId !== null && userHasPermission(user.current, 'swarm:join', managerEnvironmentId)
	);
	const candidatesQuery = createQuery(() => {
		const environmentId = managerEnvironmentId;
		return {
			queryKey: queryKeys.swarm.joinCandidates(environmentId, user.current),
			enabled: canDiscover,
			queryFn: async ({ signal }): Promise<SwarmJoinCandidate[]> => {
				await environmentStore.ready;
				signal.throwIfAborted();
				if (!environmentId) return [];
				return swarmService.getSwarmJoinCandidates(environmentId, { signal, suppressAccessDeniedToast: true });
			},
			retry: false,
			throwOnError: false,
			refetchOnWindowFocus: false,
			refetchOnReconnect: false,
			gcTime: 0
		};
	});
	const candidates = $derived.by(() => {
		if (!canDiscover || candidatesQuery.isError) return [];
		return candidatesQuery.data ?? [];
	});

	return {
		get managerEnvironmentId() {
			return managerEnvironmentId;
		},
		isCandidate(environmentId: string) {
			return candidates.some((candidate) => candidate.environmentId === environmentId);
		},
		async refresh() {
			if (canDiscover) await candidatesQuery.refetch();
		}
	};
}
