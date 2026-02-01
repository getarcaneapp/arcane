import { createContainerStatsWebSocket } from '$lib/utils/ws';
import type { ContainerStats } from '$lib/types/container.type';
import {
	calculateCPUPercent,
	calculateMemoryPercent,
	calculateMemoryUsage
} from '$lib/utils/container-stats.utils';
import type { ReconnectingWebSocket } from '$lib/utils/ws';

export class ContainerStatsManager {
	private connections = new Map<string, ReconnectingWebSocket<ContainerStats>>();
	private stats = $state(new Map<string, ContainerStats>());
	private loadingStates = $state(new Map<string, boolean>());

	connect(containerId: string, envId: string): void {
		if (this.connections.has(containerId)) return;

		this.loadingStates = new Map(this.loadingStates).set(containerId, true);

		const ws = createContainerStatsWebSocket({
			getEnvId: () => envId,
			containerId,
			onMessage: (data: ContainerStats) => {
				this.stats = new Map(this.stats).set(containerId, data);
				this.loadingStates = new Map(this.loadingStates).set(containerId, false);
			},
			onError: (err) => {
				console.error(`[ContainerStatsManager] Stats error for container ${containerId}:`, err);
				this.loadingStates = new Map(this.loadingStates).set(containerId, false);
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
			const newStats = new Map(this.stats);
			newStats.delete(containerId);
			this.stats = newStats;
			const newLoadingStates = new Map(this.loadingStates);
			newLoadingStates.delete(containerId);
			this.loadingStates = newLoadingStates;
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

	destroy(): void {
		for (const ws of this.connections.values()) {
			ws.close();
		}
		this.connections.clear();
		this.stats = new Map();
		this.loadingStates = new Map();
	}
}
