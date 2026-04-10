<script lang="ts">
	import { createContainerStatsWebSocket, type ReconnectingWebSocket } from '$lib/utils/ws';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import type { ContainerStats as ContainerStatsType } from '$lib/types/container.type';
	import { invalidateAll } from '$app/navigation';
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

	let statsWebSocket: ReconnectingWebSocket<ContainerStatsType> | null = null;
	let isConnecting = false;

	async function startStatsStream() {
		if (!enabled || isConnecting || statsWebSocket || !containerId) {
			return;
		}

		hasInitialStatsLoaded = false;
		isConnecting = true;
		try {
			const envId = await environmentStore.getCurrentEnvironmentId();

			const ws = createContainerStatsWebSocket({
				getEnvId: () => envId,
				containerId,
				onMessage: (statsData) => {
					if (statsData.removed) {
						void invalidateAll();
						return;
					}

					stats = statsData;
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
		} catch (error) {
			console.error('Failed to connect to stats stream:', error);
			isConnecting = false;
		}
	}

	function closeStatsStream() {
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
