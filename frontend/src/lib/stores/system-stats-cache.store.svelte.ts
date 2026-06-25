import { setContext, getContext } from 'svelte';
import type { SystemStats } from '$lib/types/shared';

type CachedStats = {
	stats: SystemStats;
	timestamp: number;
};

const CACHE_TTL_MS = 5 * 60 * 1000; // 5 minutes
const CACHE_CONTEXT_KEY = Symbol('system-stats-cache');

export function createSystemStatsCacheStore() {
	let cache = $state<Record<string, CachedStats>>({});

	return {
		get(environmentId: string): SystemStats | null {
			const entry = cache[environmentId];
			if (!entry) return null;
			if (Date.now() - entry.timestamp > CACHE_TTL_MS) {
				delete cache[environmentId];
				return null;
			}
			return entry.stats;
		},
		set(environmentId: string, stats: SystemStats) {
			cache[environmentId] = {
				stats,
				timestamp: Date.now()
			};
		},
		clear() {
			cache = {};
		}
	};
}

export type SystemStatsCacheStore = ReturnType<typeof createSystemStatsCacheStore>;

export function setSystemStatsCacheContext(): SystemStatsCacheStore {
	const store = createSystemStatsCacheStore();
	setContext(CACHE_CONTEXT_KEY, store);
	return store;
}

export function getSystemStatsCacheContext(): SystemStatsCacheStore {
	return getContext<SystemStatsCacheStore>(CACHE_CONTEXT_KEY);
}
