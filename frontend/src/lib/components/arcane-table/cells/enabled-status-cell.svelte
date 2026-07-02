<script lang="ts">
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { m } from '$lib/paraglide/messages';

	let { value }: { value: unknown } = $props();
	const enabled = $derived(Boolean(value));
</script>

<StatusBadge variant={enabled ? 'green' : 'red'} text={enabled ? m.common_enabled() : m.common_disabled()} />
