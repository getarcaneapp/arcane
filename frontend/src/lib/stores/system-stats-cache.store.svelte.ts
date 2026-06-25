import type { SystemStats } from '$lib/types/shared';

type CachedStats = {
	stats: SystemStats;
	timestamp: number;
};

const CACHE_TTL_MS = 5 * 60 * 1000; // 5 minutes

function createSystemStatsCacheStore() {
	let cache: Record<string, CachedStats> = {};

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

export const systemStatsCacheStore = createSystemStatsCacheStore();
