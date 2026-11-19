import { createQuery } from '@tanstack/svelte-query';

import { swarmJoinCandidateManagersQuery } from '#lib/query/swarm.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import { userHasPermission } from '#lib/utils/auth.js';

export function useEasyJoinCandidates() {
	const user = userStore;
	const managerEnvironments = $derived(
		environmentStore.shown.filter((env) => userHasPermission(user.current, 'swarm:join', env.id))
	);
	const managerEnvironmentIds = $derived(managerEnvironments.map((env) => env.id));
	const candidatesQuery = createQuery(() => ({
		...swarmJoinCandidateManagersQuery(managerEnvironments, user.current),
		enabled: managerEnvironments.length > 0,
		retry: false,
		throwOnError: false,
		refetchOnWindowFocus: false,
		refetchOnReconnect: false,
		gcTime: 0
	}));
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
