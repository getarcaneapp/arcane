import { onMount } from 'svelte';

import { environmentStore } from '#lib/stores/environment.store.svelte.js';

// Refresh when the selected or shown environments change after the initial selection.
export function useEnvironmentRefresh(onRefresh: () => void | Promise<void>) {
	onMount(() => {
		const scopeKey = () => `${environmentStore.selected?.id}|${environmentStore.shownKey}`;
		let lastScopeKey = environmentStore.selected ? scopeKey() : null;

		return environmentStore.subscribeSelected((env) => {
			if (!env) return;

			const previousScopeKey = lastScopeKey;
			lastScopeKey = scopeKey();
			if (previousScopeKey !== null && previousScopeKey !== lastScopeKey) onRefresh();
		});
	});
}
