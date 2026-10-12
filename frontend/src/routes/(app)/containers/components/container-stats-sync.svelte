<script lang="ts">
	import type { ContainerStatsManager } from './container-stats-manager.svelte';

	let {
		statsManager,
		targetIds
	}: {
		statsManager: ContainerStatsManager;
		targetIds: Set<string>;
	} = $props();

	$effect(() => {
		statsManager.targetIds = targetIds;
	});
</script>
