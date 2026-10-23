import { createQuery } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { settingsService } from '#lib/services/settings-service.js';
import settingsStore from '#lib/stores/config-store.svelte.js';
import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';

export function useEnvironmentSettings(envId: string | (() => string)) {
	const environmentId = typeof envId === 'string' ? () => envId : envId;
	const query = createQuery(() => ({
		queryKey: queryKeys.settings.byEnvironment(environmentId()),
		queryFn: () => settingsService.getReadableSettings(environmentId()),
		enabled: environmentId() !== LOCAL_DOCKER_ENVIRONMENT_ID
	}));
	return {
		get current() {
			return environmentId() === LOCAL_DOCKER_ENVIRONMENT_ID ? settingsStore.resolved : query.data;
		}
	};
}
