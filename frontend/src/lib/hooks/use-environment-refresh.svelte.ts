import { onMount } from 'svelte';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';

// Refresh only when the selected environment ID changes after initial selection.
export function useEnvironmentRefresh(onRefresh: () => void | Promise<void>) {
	onMount(() => {
		let lastEnvId = environmentStore.selected?.id ?? null;

		return environmentStore.subscribeSelected((env) => {
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
	});
}
