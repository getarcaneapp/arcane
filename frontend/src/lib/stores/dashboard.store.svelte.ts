import { browser } from '$app/env';
import { dashboardService } from '$lib/services/dashboard-service';
import { LOCAL_DOCKER_ENVIRONMENT_ID } from '$lib/stores/environment.store.svelte';
import {
	createEnvironmentStreamStore,
	environmentDisplayName,
	type StreamEnvStateBase
} from '$lib/stores/environment-stream.svelte';
import type { DashboardSnapshot, DashboardStreamErrorCode, DashboardStreamEvent } from '$lib/types/shared';
import type { Environment } from '$lib/types/environment';
import userStore from '$lib/stores/user-store';

type DashboardEnvironmentState = StreamEnvStateBase & {
	snapshot: DashboardSnapshot | null;
	// hasLoaded flips on the first-ever snapshot and never back: later errors
	// keep showing the last-known data instead of skeletons or zeros.
	hasLoaded: boolean;
	errorCode?: DashboardStreamErrorCode;
};

function createDashboardStore() {
	let started = false;
	let debugAllGood = false;

	const core = createEnvironmentStreamStore<DashboardEnvironmentState, DashboardStreamEvent>({
		label: 'Dashboard',
		includeEnvironment: (environment) => userStore.hasPermission('dashboard:read', environment.id),
		subscribeEnvironmentFilter: (reconcile) => userStore.subscribe(reconcile),
		refreshOnStart: true,
		clearErrorExtra: { errorCode: undefined },
		createEnvironmentState(environment: Pick<Environment, 'id' | 'name'>): DashboardEnvironmentState {
			return {
				id: environment.id || LOCAL_DOCKER_ENVIRONMENT_ID,
				name: environmentDisplayName(environment),
				snapshot: null,
				hasLoaded: false,
				loading: true,
				streamError: false
			};
		},
		openStream: (signal) => dashboardService.openDashboardStream(signal, debugAllGood),
		applyEvent(environmentId, event) {
			switch (event.type) {
				case 'snapshot':
					if (event.snapshot) {
						replaceEnvironmentSnapshotInternal(environmentId, event.snapshot);
					}
					break;
				case 'pending':
					// The server confirms this environment is covered; the first
					// snapshot or error for it will follow.
					break;
				case 'error':
					core.setEnvironmentError(environmentId, new Error(event.error || 'Dashboard stream error'), {
						errorCode: event.errorCode
					});
					break;
			}
		},
		async fetchSnapshot(environmentId, generation) {
			try {
				const snapshot = await dashboardService.getDashboardForEnvironment(environmentId, { debugAllGood });
				// The environment can be removed while the fetch is in-flight; don't resurrect it.
				if (!core.isCurrentGeneration(generation) || !core.environmentState(environmentId)) {
					return;
				}
				replaceEnvironmentSnapshotInternal(environmentId, snapshot);
			} catch (error) {
				if (core.isCurrentGeneration(generation) && core.environmentState(environmentId)) {
					console.warn('Failed to refresh dashboard snapshot:', error);
					core.setEnvironmentError(environmentId, error, { errorCode: undefined });
				}
			}
		}
	});

	function replaceEnvironmentSnapshotInternal(environmentId: string, snapshot: DashboardSnapshot) {
		// Snapshots can still arrive (stream or in-flight REST) after the
		// environment was removed locally; don't resurrect it.
		if (!core.environmentState(environmentId)) {
			return;
		}
		core.updateEnvironmentState(environmentId, (state) => ({
			...state,
			snapshot,
			hasLoaded: true,
			loading: false,
			streamError: false,
			errorMessage: undefined,
			errorCode: undefined
		}));
	}

	return {
		get connected(): boolean {
			return core.streamConnected;
		},
		get streamFailed(): boolean {
			return core.streamFailed;
		},
		getEnvironmentState(environmentId: string): DashboardEnvironmentState | null {
			return core.environmentState(environmentId) ?? null;
		},
		isSnapshotLoading(environmentId: string): boolean {
			const state = core.environmentState(environmentId);
			return Boolean(state && state.loading && !state.hasLoaded);
		},
		start: async (options?: { debugAllGood?: boolean }) => {
			const nextDebugAllGood = options?.debugAllGood ?? debugAllGood;
			if (!browser) {
				return;
			}
			if (started) {
				if (nextDebugAllGood === debugAllGood) {
					return;
				}
				// The flag is encoded in the stream URL; restart to apply it.
				debugAllGood = nextDebugAllGood;
				core.restartStream();
				return;
			}

			started = true;
			debugAllGood = nextDebugAllGood;
			await core.start();
		},
		stop: (options?: { resetState?: boolean }) => {
			const wasStarted = started;
			started = false;
			core.stop({ resetState: options?.resetState, resetStreamFailed: true });
			return wasStarted;
		},
		refresh: () => core.refresh(),
		retryStream: () => core.retryStream()
	};
}

export const dashboardStore = createDashboardStore();
