import { SvelteMap, SvelteSet } from 'svelte/reactivity';

import type { ContainerStats } from '#lib/types/docker.js';
import { calculateCPUPercent, calculateMemoryPercent, calculateMemoryUsage } from '#lib/utils/docker.js';
import { parseRowKey } from '#lib/utils/environments.js';
import { createContainerStatsWebSocket } from '#lib/utils/ws.js';
import type { ReconnectingWebSocket } from '#lib/utils/ws.js';

export class ContainerStatsManager {
	private connections = new SvelteMap<string, ReconnectingWebSocket<ContainerStats>>();
	private stats = new SvelteMap<string, ContainerStats>();
	private loadingStates = new SvelteMap<string, boolean>();
	private errorStates = new SvelteMap<string, boolean>();
	private desiredIds = new SvelteSet<string>();

	/** Keyed by table row key; the row's environment is decoded only for the websocket URL. */
	connect(containerId: string): void {
		if (this.connections.has(containerId)) return;
		const { id, environmentId } = parseRowKey(containerId);
		if (!environmentId) return;

		this.loadingStates.set(containerId, true);

		const ws = createContainerStatsWebSocket({
			getEnvId: () => environmentId,
			containerId: id,
			onMessage: (data: ContainerStats) => {
				this.stats.set(containerId, data);
				this.loadingStates.set(containerId, false);
				this.errorStates.delete(containerId);
			},
			onError: () => {
				this.stats.delete(containerId);
				this.loadingStates.set(containerId, false);
				this.errorStates.set(containerId, true);
			},
			shouldReconnect: () => this.connections.has(containerId)
		});

		ws.connect();
		this.connections.set(containerId, ws);
	}

	disconnect(containerId: string): void {
		const ws = this.connections.get(containerId);
		if (ws) {
			ws.close();
			this.connections.delete(containerId);
			this.stats.delete(containerId);
			this.loadingStates.delete(containerId);
			this.errorStates.delete(containerId);
		}
	}

	getCPUPercent(containerId: string): number | undefined {
		const stats = this.stats.get(containerId);
		if (!stats) return undefined;
		return calculateCPUPercent(stats);
	}

	getMemoryPercent(containerId: string): number | undefined {
		const stats = this.stats.get(containerId);
		if (!stats) return undefined;
		return calculateMemoryPercent(stats);
	}

	getMemoryUsage(containerId: string): { usage: number; limit: number } | undefined {
		const stats = this.stats.get(containerId);
		if (!stats) return undefined;
		return {
			usage: calculateMemoryUsage(stats),
			limit: stats.memory_stats?.limit || 0
		};
	}

	isLoading(containerId: string): boolean {
		return this.loadingStates.get(containerId) ?? false;
	}

	hasError(containerId: string): boolean {
		return this.errorStates.get(containerId) ?? false;
	}

	hasConnection(containerId: string): boolean {
		return this.connections.has(containerId);
	}

	getConnectedIds(): string[] {
		return Array.from(this.connections.keys());
	}

	/** Table row keys; each container streams from its row's environment. */
	set targetIds(value: Set<string>) {
		this.desiredIds = new SvelteSet(value);
		this.syncConnections();
	}

	private syncConnections(): void {
		const connectedIds = new SvelteSet(this.connections.keys());
		for (const id of this.desiredIds) {
			if (!connectedIds.has(id)) {
				this.connect(id);
			}
		}

		for (const id of connectedIds) {
			if (!this.desiredIds.has(id)) {
				this.disconnect(id);
			}
		}
	}

	destroy(): void {
		for (const ws of this.connections.values()) {
			ws.close();
		}
		this.connections.clear();
		this.stats.clear();
		this.loadingStates.clear();
		this.errorStates.clear();
	}
}
