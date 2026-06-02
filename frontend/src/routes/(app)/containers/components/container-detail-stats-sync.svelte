<script lang="ts">
	import { createContainerStatsWebSocket, type ReconnectingWebSocket } from '$lib/utils/ws';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import type { ContainerStats as ContainerStatsType } from '$lib/types/docker';
	import { goto } from '$app/navigation';
	import { queryKeys } from '$lib/query/query-keys';
	import { useQueryClient } from '@tanstack/svelte-query';
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

	const queryClient = useQueryClient();

	void stats;
	void hasInitialStatsLoaded;

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
						void handleContainerRemoved(envId);
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

	async function handleContainerRemoved(envId: string) {
		await Promise.all([
			queryClient.invalidateQueries({ queryKey: queryKeys.containers.all }),
			queryClient.invalidateQueries({ queryKey: queryKeys.containers.statusCounts(envId) }),
			containerId
				? queryClient.invalidateQueries({ queryKey: queryKeys.containers.detail(envId, containerId) })
				: Promise.resolve()
		]);
		await goto('/containers');
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
