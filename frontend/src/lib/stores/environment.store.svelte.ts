import { untrack } from 'svelte';

import type { Environment } from '#lib/types/environment.js';
import { isEnvironmentOnline } from '#lib/utils/docker.js';

export const LOCAL_DOCKER_ENVIRONMENT_ID = '0';

export type EnvironmentRef = Pick<Environment, 'id' | 'name'>;

function createEnvironmentManagementStore() {
	let _availableEnvironments = $state<Environment[]>([]);
	let _initialized = false;
	const _availableListeners = new Set<(environments: Environment[]) => void>();

	let _resolveReadyPromiseFunction: () => void;
	const _readyPromise = new Promise<void>((resolve) => {
		_resolveReadyPromiseFunction = resolve;
	});

	function _updateAvailable(environments: Environment[]): Environment[] {
		const sorted = [...environments].sort((a, b) => {
			if (a.id === LOCAL_DOCKER_ENVIRONMENT_ID) return -1;
			if (b.id === LOCAL_DOCKER_ENVIRONMENT_ID) return 1;
			return 0;
		});
		_availableEnvironments = sorted;
		untrack(() => {
			for (const listener of _availableListeners) listener(sorted);
		});
		return sorted;
	}

	return {
		get available(): Environment[] {
			return _availableEnvironments;
		},
		get shown(): EnvironmentRef[] {
			return _availableEnvironments
				.filter((env) => env.enabled)
				.sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id));
		},
		get shownKey(): string {
			return this.shown.map((env) => env.id).join(',');
		},
		initialize: async (environmentsData: Environment[]) => {
			_updateAvailable(environmentsData);
			if (_initialized) return;
			_initialized = true;
			_resolveReadyPromiseFunction();
		},
		applyLiveEnvironments: (environments: Environment[]): string[] => {
			const wasOffline = new Set(_availableEnvironments.filter((env) => !isEnvironmentOnline(env)).map((env) => env.id));
			return _updateAvailable(environments)
				.filter((env) => wasOffline.has(env.id) && isEnvironmentOnline(env))
				.map((env) => env.id);
		},
		subscribeAvailable(listener: (environments: Environment[]) => void): () => void {
			_availableListeners.add(listener);
			return () => {
				_availableListeners.delete(listener);
			};
		},
		getLocalEnvironment: () => _availableEnvironments.find((env) => env.id === LOCAL_DOCKER_ENVIRONMENT_ID) || null,
		ready: _readyPromise,
		getShownEnvironments: async (): Promise<EnvironmentRef[]> => {
			await _readyPromise;
			return environmentStore.shown;
		}
	};
}

export const environmentStore = createEnvironmentManagementStore();
