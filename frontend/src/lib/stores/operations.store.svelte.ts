import { browser } from '$app/env';
import { operationsService } from '$lib/services/operations-service';
import { LOCAL_DOCKER_ENVIRONMENT_ID } from '$lib/stores/environment.store.svelte';
import {
	createEnvironmentStreamStore,
	environmentDisplayName,
	type StreamEnvStateBase
} from '$lib/stores/environment-stream.svelte';
import type { Environment } from '$lib/types/environment';
import type { OperationsState, OperationsStreamErrorCode, OperationsStreamEvent } from '$lib/types/operations';
import userStore from '$lib/stores/user-store';

const environmentPermissions = [
	'image-updates:read',
	'projects:list',
	'projects:read',
	'containers:list',
	'containers:read',
	'vulnerabilities:read'
];
const globalPermissions = ['apikeys:list', 'apikeys:read'];

export type OperationsEnvironmentState = StreamEnvStateBase & {
	operations: OperationsState | null;
	hasLoaded: boolean;
	updatedAt?: string;
	errorCode?: OperationsStreamErrorCode;
};

function createOperationsStore() {
	let started = false;

	const core = createEnvironmentStreamStore<OperationsEnvironmentState, OperationsStreamEvent>({
		label: 'Operations',
		includeEnvironment: (environment) =>
			userStore.hasAnyPermission(environmentPermissions, environment.id) ||
			(environment.id === LOCAL_DOCKER_ENVIRONMENT_ID && userStore.hasAnyPermission(globalPermissions)),
		subscribeEnvironmentFilter: (reconcile) => userStore.subscribe(reconcile),
		refreshOnStart: true,
		clearErrorExtra: { errorCode: undefined },
		createEnvironmentState(environment: Pick<Environment, 'id' | 'name'>): OperationsEnvironmentState {
			return {
				id: environment.id || LOCAL_DOCKER_ENVIRONMENT_ID,
				name: environmentDisplayName(environment),
				operations: null,
				hasLoaded: false,
				loading: true,
				streamError: false
			};
		},
		openStream: (signal) => operationsService.openStream(signal),
		applyEvent(environmentId, event) {
			if (event.type === 'update' && event.state) {
				replaceOperationsStateInternal(environmentId, event.state, event.timestamp);
			} else if (event.type === 'error') {
				core.setEnvironmentError(environmentId, new Error(event.error || 'Operations stream error'), {
					errorCode: event.errorCode
				});
			}
		},
		async fetchCurrentState(environmentId, generation) {
			try {
				const state = await operationsService.getState(environmentId);
				if (!core.isCurrentGeneration(generation) || !core.environmentState(environmentId)) return;
				replaceOperationsStateInternal(environmentId, state, new Date().toISOString());
			} catch (error) {
				if (core.isCurrentGeneration(generation) && core.environmentState(environmentId)) {
					console.warn('Failed to refresh operations state:', error);
					core.setEnvironmentError(environmentId, error, { errorCode: undefined });
				}
			}
		}
	});

	function replaceOperationsStateInternal(environmentId: string, operations: OperationsState, updatedAt: string) {
		if (!core.environmentState(environmentId)) return;
		core.updateEnvironmentState(environmentId, (state) => ({
			...state,
			operations,
			hasLoaded: true,
			updatedAt,
			loading: false,
			streamError: false,
			errorMessage: undefined,
			errorCode: undefined
		}));
	}

	return {
		get environmentStates(): Record<string, OperationsEnvironmentState> {
			return core.environmentStates;
		},
		get connected(): boolean {
			return core.streamConnected;
		},
		start: async () => {
			if (!browser || started) return;
			started = true;
			await core.start();
		},
		stop: () => {
			started = false;
			return core.stop();
		},
		refresh: () => core.refresh(),
		retryStream: () => core.retryStream()
	};
}

export const operationsStore = createOperationsStore();
