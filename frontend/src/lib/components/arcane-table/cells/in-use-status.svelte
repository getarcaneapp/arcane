<script lang="ts">
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { m } from '$lib/paraglide/messages';

	let { inUse }: { inUse: boolean } = $props();
</script>

{#if inUse}
	<StatusBadge text={m.common_in_use()} variant="green" />
{:else}
	<StatusBadge text={m.common_unused()} variant="amber" />
{/if}
