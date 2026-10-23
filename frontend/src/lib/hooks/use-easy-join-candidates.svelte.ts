import { createQuery } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import { userHasPermission } from '#lib/utils/auth.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export function useEasyJoinCandidates() {
	const user = userStore;
	const managerEnvironmentIds = $derived(
		environmentStore.shown.map((env) => env.id).filter((id) => userHasPermission(user.current, 'swarm:join', id))
	);
	const candidatesQuery = createQuery(() => {
		const environmentIds = managerEnvironmentIds;
		return {
			queryKey: queryKeys.swarm.joinCandidates(environmentIds.join(','), user.current),
			enabled: environmentIds.length > 0,
			queryFn: async ({ signal }): Promise<Record<string, string>> => {
				const managers = await Promise.all(
					environmentIds.map(async (managerId) => {
						const result = await tryCatch(
							swarmService.getSwarmJoinCandidates(managerId, { signal, suppressAccessDeniedToast: true })
						);
						return (result.data ?? []).map((candidate) => [candidate.environmentId, managerId] as const);
					})
				);
				signal.throwIfAborted();
				return Object.fromEntries(managers.flat());
			},
			retry: false,
			throwOnError: false,
			refetchOnWindowFocus: false,
			refetchOnReconnect: false,
			gcTime: 0
		};
	});
	const managerByCandidate = $derived.by((): Record<string, string> => {
		if (managerEnvironmentIds.length === 0 || candidatesQuery.isError) return {};
		return candidatesQuery.data ?? {};
	});

	return {
		managerFor(environmentId: string): string | undefined {
			return managerByCandidate[environmentId];
		},
		isCandidate(environmentId: string) {
			return managerByCandidate[environmentId] !== undefined;
		},
		async refresh() {
			if (managerEnvironmentIds.length > 0) await candidatesQuery.refetch();
		}
	};
}
