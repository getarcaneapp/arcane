import { createQuery } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import { userHasPermission } from '#lib/utils/auth.js';
import { sumAcrossEnvironments } from '#lib/utils/environments.js';

export function useEasyJoinCandidates() {
	const user = userStore;
	const managerEnvironments = $derived(
		environmentStore.shown.filter((env) => userHasPermission(user.current, 'swarm:join', env.id))
	);
	const managerEnvironmentIds = $derived(managerEnvironments.map((env) => env.id));
	const candidatesQuery = createQuery(() => {
		const environments = managerEnvironments;
		return {
			queryKey: queryKeys.swarm.joinCandidates(environments.map((env) => env.id).join(','), user.current),
			enabled: environments.length > 0,
			queryFn: async ({ signal }): Promise<Record<string, string>> => {
				const { byEnvironment } = await sumAcrossEnvironments(
					environments,
					(scope, config) => swarmService.getSwarmJoinCandidates(scope, { ...config, signal }),
					[]
				);
				signal.throwIfAborted();
				return Object.fromEntries(
					byEnvironment.flatMap(({ environment, data }) =>
						data.map((candidate) => [candidate.environmentId, environment.id] as const)
					)
				);
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
