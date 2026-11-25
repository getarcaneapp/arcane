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
	#visible = new Map<string, Row[]>();

	get(row: Row): string | undefined {
		const entry = this.#entries.get(rowKey(row));
		return typeof entry === 'string' ? entry : undefined;
	}

	show(rows: Row[]) {
		this.#visible = Map.groupBy(rows, (row) => row.environmentId);
		const visibleKeys = new Set(rows.map(rowKey));
		for (const { controller, keys } of this.#requests.values()) {
			if (keys.some((key) => !visibleKeys.has(key))) controller.abort();
		}
		void this.#loadVisibleEnvironments();
	}

	async retryFailures() {
		await Promise.all(this.#workers.values());
		for (const rows of this.#visible.values()) {
			for (const row of rows) {
				const key = rowKey(row);
				if (this.#entries.get(key) === failed) this.#entries.delete(key);
			}
		}
		await this.#loadVisibleEnvironments();
	}

	#loadVisibleEnvironments() {
		return Promise.all(
			Array.from(this.#visible.keys(), (environmentId) => {
				const running = this.#workers.get(environmentId);
				if (running) return running;
				const worker = Promise.resolve().then(() => this.#loadEnvironment(environmentId));
				this.#workers.set(environmentId, worker);
				return worker;
			})
		);
	}

	async #loadEnvironment(environmentId: string) {
		try {
			while (this.#visible.has(environmentId)) {
				const rows = this.#visible.get(environmentId) ?? [];
				const batch = rows.filter((row) => !this.#entries.has(rowKey(row))).slice(0, batchSize);
				if (batch.length === 0) return;

				const keys = batch.map(rowKey);
				const controller = new AbortController();
				this.#requests.set(environmentId, { controller, keys });
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
		this.#visible.clear();
		for (const { controller } of this.#requests.values()) controller.abort();
	}
}
