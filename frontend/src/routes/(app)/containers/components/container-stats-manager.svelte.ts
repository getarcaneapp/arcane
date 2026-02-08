import { createContainerStatsWebSocket } from '$lib/utils/ws';
import type { ContainerStats } from '$lib/types/container.type';
import { calculateCPUPercent, calculateMemoryPercent, calculateMemoryUsage } from '$lib/utils/container-stats.utils';
import type { ReconnectingWebSocket } from '$lib/utils/ws';
import { SvelteMap, SvelteSet } from 'svelte/reactivity';

export class ContainerStatsManager {
	private connections = new SvelteMap<string, ReconnectingWebSocket<ContainerStats>>();
	private stats = new SvelteMap<string, ContainerStats>();
	private loadingStates = new SvelteMap<string, boolean>();
	private desiredIds = new SvelteSet<string>();
	private currentEnvId: string | null = null;

	connect(containerId: string, envId: string): void {
		if (this.connections.has(containerId)) return;

		this.loadingStates.set(containerId, true);

		const ws = createContainerStatsWebSocket({
			getEnvId: () => envId,
			containerId,
			onMessage: (data: ContainerStats) => {
				this.stats.set(containerId, data);
				this.loadingStates.set(containerId, false);
			},
			onError: (err) => {
				console.error(`[ContainerStatsManager] Stats error for container ${containerId}:`, err);
				this.loadingStates.set(containerId, false);
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

	hasConnection(containerId: string): boolean {
		return this.connections.has(containerId);
	}

	getConnectedIds(): string[] {
		return Array.from(this.connections.keys());
	}

	set envId(value: string) {
		if (this.currentEnvId === value) return;
		this.currentEnvId = value;
		this.resetConnections();
		this.syncConnections();
	}

	set targetIds(value: Set<string>) {
		this.desiredIds = new SvelteSet(value);
		this.syncConnections();
	}

	private resetConnections(): void {
		const ids = Array.from(this.connections.keys());
		for (const id of ids) {
			this.disconnect(id);
		}
	}

	private syncConnections(): void {
		if (!this.currentEnvId) return;
		const connectedIds = new SvelteSet(this.connections.keys());

		for (const id of this.desiredIds) {
			if (!connectedIds.has(id)) {
				this.connect(id, this.currentEnvId);
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
	}
}
