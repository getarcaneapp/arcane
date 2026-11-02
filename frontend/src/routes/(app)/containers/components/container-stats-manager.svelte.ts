import { SvelteMap } from 'svelte/reactivity';

import type { ContainerResourceSample, ContainerResourceSampleBatch } from '#lib/types/docker.js';
import { parseRowKey } from '#lib/utils/environments.js';
import { createContainersStatsWebSocket } from '#lib/utils/ws.js';
import type { ReconnectingWebSocket } from '#lib/utils/ws.js';

/** The server samples at most this many containers per environment stream. */
const MAX_SUBSCRIBED_CONTAINERS = 500;
export const SAMPLE_HISTORY_LIMIT = 60;

interface EnvironmentStream {
	ws: ReconnectingWebSocket<ContainerResourceSampleBatch>;
	/** Container ID to table row key. */
	rowKeys: Map<string, string>;
}

/** One batched stats socket per environment; samples are keyed by table row key. */
export class ContainerStatsManager {
	private streams = new Map<string, EnvironmentStream>();
	private samples = new SvelteMap<string, ContainerResourceSample>();
	private history = new SvelteMap<string, ContainerResourceSample[]>();
	private loadingStates = new SvelteMap<string, boolean>();
	private errorStates = new SvelteMap<string, boolean>();

	getCPUPercent(containerId: string): number | undefined {
		return this.samples.get(containerId)?.cpuPercent;
	}

	getMemoryUsage(containerId: string): { usage: number; limit: number } | undefined {
		const sample = this.samples.get(containerId);
		if (!sample) return undefined;
		return { usage: sample.memoryUsageBytes, limit: sample.memoryLimitBytes };
	}

	getSample(containerId: string): ContainerResourceSample | undefined {
		return this.samples.get(containerId);
	}

	getHistory(containerId: string): ContainerResourceSample[] {
		return this.history.get(containerId) ?? [];
	}

	isLoading(containerId: string): boolean {
		return this.loadingStates.get(containerId) ?? false;
	}

	hasError(containerId: string): boolean {
		return this.errorStates.get(containerId) ?? false;
	}

	/** Table row keys; each container is sampled on its row's environment stream. */
	set targetIds(value: Set<string>) {
		const byEnvironment = new Map<string, Map<string, string>>();
		for (const key of value) {
			const { id, environmentId } = parseRowKey(key);
			if (!environmentId) continue;
			const rowKeys = byEnvironment.get(environmentId) ?? new Map<string, string>();
			rowKeys.set(id, key);
			byEnvironment.set(environmentId, rowKeys);
		}

		for (const [environmentId, stream] of this.streams) {
			if (byEnvironment.has(environmentId)) continue;
			stream.ws.close();
			this.streams.delete(environmentId);
			this.forget(stream.rowKeys.values());
		}

		for (const [environmentId, rowKeys] of byEnvironment) {
			const stream = this.streams.get(environmentId);
			if (!stream) {
				for (const key of rowKeys.values()) this.loadingStates.set(key, true);
				this.open(environmentId, rowKeys);
				continue;
			}
			const unchanged = stream.rowKeys.size === rowKeys.size && [...rowKeys.keys()].every((id) => stream.rowKeys.has(id));
			if (unchanged) continue;

			this.forget([...stream.rowKeys].filter(([id]) => !rowKeys.has(id)).map(([, key]) => key));
			for (const [id, key] of rowKeys) {
				if (!stream.rowKeys.has(id)) this.loadingStates.set(key, true);
			}
			stream.rowKeys = rowKeys;
			this.subscribe(stream);
		}
	}

	destroy(): void {
		for (const stream of this.streams.values()) stream.ws.close();
		this.streams.clear();
		this.samples.clear();
		this.history.clear();
		this.loadingStates.clear();
		this.errorStates.clear();
	}

	private open(environmentId: string, rowKeys: Map<string, string>): void {
		const ws = createContainersStatsWebSocket({
			envId: environmentId,
			onOpen: () => {
				const stream = this.streams.get(environmentId);
				if (stream?.ws === ws) this.subscribe(stream);
			},
			onMessage: (batch) => {
				const stream = this.streams.get(environmentId);
				if (stream?.ws === ws) this.apply(stream, batch);
			},
			onError: () => {
				const stream = this.streams.get(environmentId);
				if (stream?.ws === ws) this.markUnavailable(stream);
			},
			shouldReconnect: () => this.streams.get(environmentId)?.ws === ws
		});
		this.streams.set(environmentId, { ws, rowKeys });
		void ws.connect();
	}

	/** Sends the stream's container set; a closed socket resends it from onOpen. */
	private subscribe(stream: EnvironmentStream): void {
		const containerIds = [...stream.rowKeys.keys()].slice(0, MAX_SUBSCRIBED_CONTAINERS);
		stream.ws.send(JSON.stringify({ containerIds }));
	}

	/** Writes one batch in a single synchronous pass so it flushes as one update. */
	private apply(stream: EnvironmentStream, batch: ContainerResourceSampleBatch): void {
		if (batch.error) {
			this.markUnavailable(stream);
			return;
		}
		for (const [id, key] of stream.rowKeys) {
			const sample = batch.samples?.[id];
			if (sample) {
				const history = this.history.get(key) ?? [];
				if (history.at(-1)?.sampleTime !== sample.sampleTime) {
					this.history.set(key, [...history.slice(1 - SAMPLE_HISTORY_LIMIT), sample]);
				}
				this.samples.set(key, sample);
				this.errorStates.delete(key);
			} else {
				this.samples.delete(key);
				this.history.delete(key);
			}
			this.loadingStates.set(key, false);
		}
	}

	private markUnavailable(stream: EnvironmentStream): void {
		for (const key of stream.rowKeys.values()) {
			this.samples.delete(key);
			this.history.delete(key);
			this.loadingStates.set(key, false);
			this.errorStates.set(key, true);
		}
	}

	private forget(keys: Iterable<string>): void {
		for (const key of keys) {
			this.samples.delete(key);
			this.history.delete(key);
			this.loadingStates.delete(key);
			this.errorStates.delete(key);
		}
	}
}
