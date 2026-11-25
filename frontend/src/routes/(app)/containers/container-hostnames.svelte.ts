import { SvelteMap } from 'svelte/reactivity';

import { containerService } from '#lib/services/container-service.js';
import type { ContainerSummary } from '#lib/types/container.js';
import { type EnvironmentRow, rowKey } from '#lib/utils/environments.js';

const batchSize = 100;
const failed = Symbol('failed');
type Row = EnvironmentRow<ContainerSummary>;

export class ContainerHostnames {
	readonly #entries = new SvelteMap<string, string | typeof failed>();
	readonly #workers = new Map<string, Promise<void>>();
	readonly #requests = new Map<string, { controller: AbortController; keys: string[] }>();
	readonly #rendered = new Map<string, { row: Row; count: number }>();
	#sync: Promise<void> | undefined;

	get(row: Row): string | undefined {
		const entry = this.#entries.get(rowKey(row));
		return typeof entry === 'string' ? entry : undefined;
	}

	watch(row: Row) {
		const key = rowKey(row);
		this.#rendered.set(key, { row, count: (this.#rendered.get(key)?.count ?? 0) + 1 });
		void this.#scheduleSync();
		return () => {
			const count = (this.#rendered.get(key)?.count ?? 1) - 1;
			if (count > 0) {
				this.#rendered.set(key, { row, count });
			} else {
				this.#rendered.delete(key);
			}
			void this.#scheduleSync();
		};
	}

	async retryFailures() {
		await this.#scheduleSync();
		await Promise.all(this.#workers.values());
		for (const [key, entry] of this.#entries) {
			if (entry === failed) this.#entries.delete(key);
		}
		await this.#loadRenderedEnvironments();
	}

	#scheduleSync() {
		this.#sync ??= Promise.resolve().then(() => {
			this.#sync = undefined;
			for (const { controller, keys } of this.#requests.values()) {
				if (!keys.some((key) => this.#rendered.has(key))) controller.abort();
			}
			void this.#loadRenderedEnvironments();
		});
		return this.#sync;
	}

	#loadRenderedEnvironments() {
		const environmentIds = new Set(Array.from(this.#rendered.values(), ({ row }) => row.environmentId));
		return Promise.all(
			Array.from(environmentIds, (environmentId) => {
				const running = this.#workers.get(environmentId);
				if (running) return running;
				const worker = this.#loadEnvironment(environmentId);
				this.#workers.set(environmentId, worker);
				return worker;
			})
		);
	}

	async #loadEnvironment(environmentId: string) {
		try {
			await Promise.resolve();
			while (true) {
				const batch = Array.from(this.#rendered.values(), ({ row }) => row)
					.filter((row) => row.environmentId === environmentId && !this.#entries.has(rowKey(row)))
					.slice(0, batchSize);
				if (batch.length === 0) return;

				const controller = new AbortController();
				this.#requests.set(environmentId, { controller, keys: batch.map(rowKey) });
				let hostnames: Record<string, string> = {};
				try {
					hostnames = await containerService.getSwarmTaskHostnames(
						environmentId,
						batch.map((row) => row.id),
						controller.signal
					);
				} catch {
					// Keep Docker names visible and allow Refresh to retry failed lookups.
				} finally {
					this.#requests.delete(environmentId);
				}
				if (controller.signal.aborted) continue;
				for (const row of batch) {
					const hostname = Object.hasOwn(hostnames, row.id) ? hostnames[row.id] : undefined;
					this.#entries.set(rowKey(row), hostname ?? failed);
				}
			}
		} finally {
			this.#workers.delete(environmentId);
		}
	}

	destroy() {
		this.#rendered.clear();
		for (const { controller } of this.#requests.values()) controller.abort();
	}
}
