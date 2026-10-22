import { createQuery } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { settingsService } from '#lib/services/settings-service.js';
import settingsStore from '#lib/stores/config-store.svelte.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';

// Settings of a resource's environment, which can differ from the selected one; undefined until loaded.
export function useEnvironmentSettings(environmentId: () => string) {
	const query = createQuery(() => ({
		queryKey: queryKeys.settings.byEnvironment(environmentId()),
		queryFn: () => settingsService.getReadableSettings(environmentId()),
		enabled: environmentId() !== environmentStore.selected?.id
	}));
	return {
		get current() {
			return environmentId() === environmentStore.selected?.id ? settingsStore.resolved : query.data;
		}
	};
}
