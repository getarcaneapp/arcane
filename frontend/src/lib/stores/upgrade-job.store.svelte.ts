import { browser } from '$app/env';
import { refreshAll } from '$app/navigation';

import BaseAPIService from '#lib/services/api-service.js';
import systemUpgradeService from '#lib/services/api/system-upgrade-service.js';
import { STREAM_CHANNEL_UPGRADE } from '#lib/services/stream-service.js';
import { clientStream } from '#lib/stores/client-stream.svelte.js';
import { versionStore } from '#lib/stores/version.store.svelte.js';
import type { UpgradeJob } from '#lib/types/system-upgrade.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export const MANAGER_ENVIRONMENT_ID = '0';

export function isActiveUpgradeJob(job: UpgradeJob | null | undefined): job is UpgradeJob {
	return job?.status === 'running' || job?.status === 'pending_restart';
}

function isUpgradeJob(value: unknown): value is UpgradeJob {
	return !!value && typeof value === 'object' && typeof (value as UpgradeJob).id === 'string';
}

function createUpgradeJobStoreInternal() {
	let job = $state.raw<UpgradeJob | null>(null);
	let followedJobId: string | null = null;
	let followedActive = $state(false);
	let followsManager = $state(false);
	let settledAtSnapshot = $state(0);
	let watchers = 0;
	let unsubscribeChannel: (() => void) | null = null;
	let releaseFollowWatch: (() => void) | null = null;

	function apply(next: UpgradeJob) {
		job = next;
		if (next.id !== followedJobId || !followedActive || (next.status !== 'completed' && next.status !== 'failed')) return;
		followedActive = false;
		followsManager = (next.results ?? []).some(
			(result) =>
				result.environmentId === MANAGER_ENVIRONMENT_ID && (result.status === 'updated' || result.status === 'triggered')
		);
		settledAtSnapshot = versionStore.snapshots;
		BaseAPIService.setUpgradeInProgress(false);
		releaseFollowWatch?.();
		releaseFollowWatch = null;
		void refreshAll();
	}

	async function load(): Promise<UpgradeJob | null> {
		const result = await tryCatch(systemUpgradeService.getCurrentUpgradeJob());
		if (result.error !== null) return null;
		apply(result.data);
		return result.data;
	}

	function watch(): () => void {
		if (!browser) return () => {};
		watchers++;
		if (watchers === 1) {
			unsubscribeChannel = clientStream.subscribe(STREAM_CHANNEL_UPGRADE, {
				onEvent: (payload) => {
					if (isUpgradeJob(payload)) apply(payload);
				},
				onConnected: () => void load()
			});
		}
		return () => {
			watchers--;
			if (watchers > 0) return;
			unsubscribeChannel?.();
			unsubscribeChannel = null;
		};
	}

	return {
		get job(): UpgradeJob | null {
			return job;
		},
		get reloadsOnBuildChange(): boolean {
			return followsManager && (followedActive || versionStore.snapshots <= settledAtSnapshot + 1);
		},
		load,
		follow(next: UpgradeJob) {
			followedJobId = next.id;
			followedActive = true;
			followsManager = (next.results ?? []).some((result) => result.environmentId === MANAGER_ENVIRONMENT_ID);
			BaseAPIService.setUpgradeInProgress(followsManager);
			releaseFollowWatch ??= watch();
			apply(next);
		},
		watch
	};
}

export const upgradeJobStore = createUpgradeJobStoreInternal();
