import { PersistedState } from 'runed';
import { goto, invalidateAll } from '$app/navigation';
import { page } from '$app/state';
import type { Environment } from '$lib/types/environment.type';

export const LOCAL_DOCKER_ENVIRONMENT_ID = '0';

function getResourceListPage(): string | null {
	const routeId = page.route?.id;
	if (!routeId) return null;

	// Check if route has a dynamic segment (contains [...] pattern)
	// and is a resource detail page (not settings, environments management, etc.)
	const resourcePrefixes = ['/containers', '/images', '/projects', '/networks', '/volumes'];

	for (const prefix of resourcePrefixes) {
		// Match routes like /containers/[containerId] or /(app)/containers/[containerId]
		// but not /containers or /containers/components/...
		const pattern = prefix + '/[';
		if (routeId.includes(pattern) && !routeId.includes('/components/')) {
			return prefix;
		}
	}

	return null;
}

function createEnvironmentManagementStore() {
	const selectedEnvironmentId = new PersistedState<string | null>('selectedEnvironmentId', null);

	let _selectedEnvironment = $state<Environment | null>(null);
	let _availableEnvironments = $state<Environment[]>([]);
	let _initialized = false;
	let _initializedWithData = false;

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
		return sorted;
	}

	function _selectInitialEnvironment(available: Environment[]): Environment | null {
		const savedId = selectedEnvironmentId.current;

		if (savedId) {
			const found = available.find((env) => env.id === savedId);
			if (found && found.enabled) {
				_selectedEnvironment = found;
				return found;
			}
		}

		const localEnv = available.find((env) => env.id === LOCAL_DOCKER_ENVIRONMENT_ID);
		if (localEnv && localEnv.enabled) {
			_selectedEnvironment = localEnv;
			return localEnv;
		}

		const firstEnabled = available.find((env) => env.enabled);
		if (firstEnabled) {
			_selectedEnvironment = firstEnabled;
			return firstEnabled;
		}

		_selectedEnvironment = null;
		return null;
	}

	return {
		get selected(): Environment | null {
			return _selectedEnvironment;
		},
		get available(): Environment[] {
			return _availableEnvironments;
		},
		initialize: async (environmentsData: Environment[]) => {
			const available = _updateAvailable(environmentsData);
			const hasRealEnvironments = environmentsData.length > 0;

			if (!_initialized) {
				_selectInitialEnvironment(available);
				_initialized = true;
				if (hasRealEnvironments) {
					_initializedWithData = true;
				}
				_resolveReadyPromiseFunction();
			} else if (hasRealEnvironments && !_initializedWithData) {
				_selectInitialEnvironment(available);
				_initializedWithData = true;
			} else {
				// Update the selected environment's data if it exists
				if (_selectedEnvironment) {
					const updated = available.find((env) => env.id === _selectedEnvironment!.id);
					if (updated) {
						_selectedEnvironment = updated;
						// If the current environment was disabled, switch to an enabled one
						if (!updated.enabled) {
							_selectInitialEnvironment(available);
						}
					} else {
						// Environment no longer exists, select a new one
						_selectInitialEnvironment(available);
					}
				} else if (available.length > 0) {
					_selectInitialEnvironment(available);
				}
			}
		},
		setEnvironment: async (environment: Environment) => {
			if (!environment.enabled) return;
			if (_selectedEnvironment?.id !== environment.id) {
				_selectedEnvironment = environment;
				selectedEnvironmentId.current = environment.id;

				// Check if we're on a resource detail page (e.g., /containers/abc123)
				// These pages show environment-specific resources that won't exist in the new environment
				// Navigate to the corresponding list page to avoid 500 errors
				const listPage = getResourceListPage();
				if (listPage) {
					await goto(listPage);
				} else {
					await invalidateAll();
				}
			}
		},
		isInitialized: () => _initialized,
		getLocalEnvironment: () => _availableEnvironments.find((env) => env.id === LOCAL_DOCKER_ENVIRONMENT_ID) || null,
		ready: _readyPromise,
		getCurrentEnvironmentId: async (): Promise<string> => {
			await _readyPromise;
			return _selectedEnvironment ? _selectedEnvironment.id : LOCAL_DOCKER_ENVIRONMENT_ID;
		}
	};
}

export const environmentStore = createEnvironmentManagementStore();
