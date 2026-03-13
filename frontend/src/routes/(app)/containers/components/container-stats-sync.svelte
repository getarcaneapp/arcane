<script lang="ts">
	import type { ContainerStatsManager } from './container-stats-manager.svelte';

	let {
		statsManager,
		envId,
		targetIds
	}: {
		statsManager: ContainerStatsManager;
		envId: string;
		targetIds: Set<string>;
	} = $props();

	$effect(() => {
		statsManager.envId = envId;
		statsManager.targetIds = targetIds;
	});
</script>
