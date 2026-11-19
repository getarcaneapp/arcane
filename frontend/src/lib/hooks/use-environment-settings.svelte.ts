import { createQuery } from '@tanstack/svelte-query';

import { mergedSettingsQuery } from '#lib/query/settings.js';
import settingsStore from '#lib/stores/config-store.svelte.js';
import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';

export function useEnvironmentSettings(envId: string | (() => string)) {
	const environmentId = typeof envId === 'string' ? () => envId : envId;
	const query = createQuery(() => ({
		...mergedSettingsQuery(environmentId()),
		enabled: environmentId() !== LOCAL_DOCKER_ENVIRONMENT_ID
	}));
	return {
		get current() {
			return environmentId() === LOCAL_DOCKER_ENVIRONMENT_ID ? settingsStore.resolved : query.data;
		}
	};
}
