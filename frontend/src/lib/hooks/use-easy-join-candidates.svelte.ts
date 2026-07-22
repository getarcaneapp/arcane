import { swarmService } from '#lib/services/swarm-service';
import { environmentStore } from '#lib/stores/environment.store.svelte';
import type { SwarmJoinCandidate } from '#lib/types/swarm';

export function useEasyJoinCandidates() {
	let candidates = $state<SwarmJoinCandidate[]>([]);
	let managerEnvironmentId = $state<string | null>(null);
	let refreshVersion = $state(0);
	let requestVersion = 0;

	$effect(() => {
		const environmentId = environmentStore.selected?.id ?? null;
		void refreshVersion;
		managerEnvironmentId = environmentId;
		candidates = [];
		const currentRequest = ++requestVersion;
		if (!environmentId) return;

		void swarmService
			.getSwarmJoinCandidates(environmentId)
			.then((result) => {
				if (currentRequest === requestVersion) candidates = result;
			})
			.catch(() => {
				if (currentRequest === requestVersion) candidates = [];
			});
	});

	return {
		get managerEnvironmentId() {
			return managerEnvironmentId;
		},
		isCandidate(environmentId: string) {
			return candidates.some((candidate) => candidate.environmentId === environmentId);
		},
		refresh() {
			refreshVersion += 1;
		}
	};
}
