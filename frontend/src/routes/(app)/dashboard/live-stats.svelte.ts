import type { Environment } from '#lib/types/environment.js';
import type { DashboardLiveStatsStatus, SystemStats } from '#lib/types/shared.js';
import { hasPermission } from '#lib/utils/auth.js';
import { createStatsWebSocket, type ReconnectingWebSocket } from '#lib/utils/ws.js';

import { diskPressure, shouldLoadEnvironment, type DiskPressure } from './overview.helpers';

export type EnvironmentLiveStatsState = {
	stats: SystemStats | null;
	loading: boolean;
	failed: boolean;
	client: ReconnectingWebSocket<SystemStats> | null;
	deadline: ReturnType<typeof setTimeout> | null;
};

const LIVE_STATS_DEADLINE_MS = 10_000;

export function canLoadLiveStats(environment: Environment): boolean {
	return shouldLoadEnvironment(environment) && hasPermission('system:read', environment.id);
}

function clearLiveStatsDeadline(liveStatsState: EnvironmentLiveStatsState) {
	if (liveStatsState.deadline) {
		clearTimeout(liveStatsState.deadline);
		liveStatsState.deadline = null;
	}
}

function markLiveStatsFailed(liveStatsState: EnvironmentLiveStatsState) {
	clearLiveStatsDeadline(liveStatsState);
	liveStatsState.loading = false;
	liveStatsState.failed = true;
}

function startLiveStatsAttempt(liveStatsState: EnvironmentLiveStatsState) {
	clearLiveStatsDeadline(liveStatsState);
	liveStatsState.loading = liveStatsState.stats === null;
	liveStatsState.failed = liveStatsState.stats !== null && liveStatsState.failed;
	liveStatsState.deadline = setTimeout(() => markLiveStatsFailed(liveStatsState), LIVE_STATS_DEADLINE_MS);
}

export class DashboardLiveStats {
	#byEnvironmentId = $state<Record<string, EnvironmentLiveStatsState>>({});

	get(environmentId: string): EnvironmentLiveStatsState | null {
		return this.#byEnvironmentId[environmentId] ?? null;
	}

	#diskPressure: ReadonlyMap<string, DiskPressure> = new Map();

	readonly diskPressureByEnvironment = $derived.by((): ReadonlyMap<string, DiskPressure> => {
		const previous = this.#diskPressure;
		const next = new Map(
			Object.entries(this.#byEnvironmentId).flatMap(([environmentId, state]) => {
				const pressure = diskPressure(state.stats);
				return pressure ? [[environmentId, pressure] as const] : [];
			})
		);
		const unchanged =
			next.size === previous.size &&
			[...next].every(
				([environmentId, pressure]) =>
					previous.get(environmentId)?.percent === pressure.percent && previous.get(environmentId)?.detail === pressure.detail
			);
		if (unchanged) return previous;
		this.#diskPressure = next;
		return next;
	});

	status(environment: Environment): DashboardLiveStatsStatus {
		if (!shouldLoadEnvironment(environment)) {
			return 'live';
		}
		if (!hasPermission('system:read', environment.id)) {
			return 'denied';
		}
		const liveStatsState = this.get(environment.id);
		if (!liveStatsState) {
			return 'loading';
		}
		if (liveStatsState.failed) {
			return liveStatsState.stats ? 'stale' : 'unavailable';
		}
		return liveStatsState.loading ? 'loading' : 'live';
	}

	sync(environments: Environment[]) {
		const activeEnvironmentIds = new Set(environments.map((environment) => environment.id));
		for (const environmentId of Object.keys(this.#byEnvironmentId)) {
			if (!activeEnvironmentIds.has(environmentId)) {
				this.#remove(environmentId);
			}
		}
		for (const environment of environments) {
			this.#ensure(environment);
		}
	}

	retryFailed() {
		for (const liveStatsState of Object.values(this.#byEnvironmentId)) {
			if (!liveStatsState.failed || !liveStatsState.client) {
				continue;
			}
			startLiveStatsAttempt(liveStatsState);
			void liveStatsState.client.connect();
		}
	}

	destroy() {
		for (const environmentId of Object.keys(this.#byEnvironmentId)) {
			this.#remove(environmentId);
		}
	}

	#ensure(environment: Environment) {
		if (!canLoadLiveStats(environment)) {
			this.#remove(environment.id);
			return;
		}

		if (!this.#byEnvironmentId[environment.id]) {
			this.#byEnvironmentId[environment.id] = {
				stats: null,
				loading: true,
				failed: false,
				client: null,
				deadline: null
			};
		}

		const liveStatsState = this.#byEnvironmentId[environment.id];
		if (!liveStatsState || liveStatsState.client) {
			return;
		}

		startLiveStatsAttempt(liveStatsState);
		liveStatsState.client = createStatsWebSocket({
			getEnvId: () => environment.id,
			onMessage: (stats) => {
				clearLiveStatsDeadline(liveStatsState);
				liveStatsState.stats = stats;
				liveStatsState.loading = false;
				liveStatsState.failed = false;
			},
			onError: (error) => {
				console.error(`Stats websocket error for environment ${environment.id}:`, error);
				markLiveStatsFailed(liveStatsState);
			},
			onClose: () => markLiveStatsFailed(liveStatsState)
		});
		void liveStatsState.client.connect();
	}

	#remove(environmentId: string) {
		const liveStatsState = this.#byEnvironmentId[environmentId];
		if (!liveStatsState) {
			return;
		}

		clearLiveStatsDeadline(liveStatsState);
		liveStatsState.client?.close();
		delete this.#byEnvironmentId[environmentId];
	}
}
