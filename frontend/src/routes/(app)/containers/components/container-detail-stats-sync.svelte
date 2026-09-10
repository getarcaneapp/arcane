<script lang="ts">
	import { tryCatch } from '#lib/utils/try-catch.js';

	import { createContainerStatsWebSocket, type ReconnectingWebSocket } from '#lib/utils/ws.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { ContainerStats as ContainerStatsType, ContainerStatsHistorySample } from '#lib/types/docker.js';
	import { refreshAll } from '$app/navigation';
	import { onDestroy } from 'svelte';

	let {
		containerId,
		enabled,
		stats = $bindable<ContainerStatsType | null>(null),
		hasInitialStatsLoaded = $bindable(false)
	}: {
		containerId?: string;
		enabled: boolean;
		stats?: ContainerStatsType | null;
		hasInitialStatsLoaded?: boolean;
	} = $props();

	stats = null;
	hasInitialStatsLoaded = false;

	let statsWebSocket: ReconnectingWebSocket<ContainerStatsType> | null = null;
	let isConnecting = false;
	let generation = 0;
	let history: ContainerStatsHistorySample[] = [];
	let lastStatsRead: string | null = null;

	async function startStatsStream() {
		if (!enabled || isConnecting || statsWebSocket || !containerId) {
			return;
		}

		hasInitialStatsLoaded = false;
		isConnecting = true;
		const requestGeneration = generation;
		const requestedContainerId = containerId;
		const operationResult = await tryCatch(
			(async () => {
				const envId = await environmentStore.getCurrentEnvironmentId();
				if (requestGeneration !== generation || !enabled || requestedContainerId !== containerId) return;

				const ws = createContainerStatsWebSocket({
					getEnvId: () => envId,
					containerId: requestedContainerId,
					onMessage: (statsData) => {
						if (requestGeneration !== generation) return;
						if (statsData.removed) {
							void refreshAll();
							return;
						}

						if (statsData.statsHistory?.length) {
							history = statsData.statsHistory;
						} else if (statsData.read && statsData.read !== lastStatsRead && statsData.currentHistorySample) {
							history = [...history, statsData.currentHistorySample].slice(-30);
						}
						lastStatsRead = statsData.read;
						stats = { ...statsData, statsHistory: history };
						hasInitialStatsLoaded = true;
					},
					onOpen: () => {
						isConnecting = false;
					},
					onError: (err) => {
						console.error('Stats WebSocket error:', err);
						isConnecting = false;
					},
					onClose: () => {
						isConnecting = false;
					},
					maxBackoff: 5000,
					shouldReconnect: () => enabled
				});

				ws.connect();
				statsWebSocket = ws;
			})()
		);
		if (requestGeneration !== generation) return;
		if (operationResult.error !== null) {
			const error = operationResult.error;

			console.error('Failed to connect to stats stream:', error);
			isConnecting = false;
		}
	}

	function closeStatsStream() {
		generation += 1;
		if (statsWebSocket) {
			statsWebSocket.close();
			statsWebSocket = null;
		}

		isConnecting = false;
	}

	$effect(() => {
		if (enabled) {
			void startStatsStream();
			return;
		}

		closeStatsStream();
	});

	onDestroy(() => {
		closeStatsStream();
	});
</script>
