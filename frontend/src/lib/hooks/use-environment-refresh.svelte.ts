import { environmentStore } from '$lib/stores/environment.store.svelte';

/**
 * Creates an effect that triggers a callback when the environment changes.
 * Returns a cleanup function and tracks the last environment ID internally.
 */
export function useEnvironmentRefresh(onRefresh: () => void | Promise<void>) {
	let lastEnvId: string | null = $state(null);

	$effect(() => {
		const env = environmentStore.selected;
		if (!env) return;

		if (lastEnvId === null) {
			lastEnvId = env.id;
			return;
		}

		if (env.id !== lastEnvId) {
			lastEnvId = env.id;
			onRefresh();
		}
	});
}
