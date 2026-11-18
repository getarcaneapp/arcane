<script lang="ts">
	import { Badge } from '@arcane/ui/badge';

	import { m } from '#lib/paraglide/messages.js';

	let { inUse }: { inUse: boolean } = $props();
</script>

{#if inUse}
	<Badge variant="green" minWidth="20">{m.common_in_use()}</Badge>
{:else}
	<Badge variant="amber" minWidth="20">{m.common_unused()}</Badge>
{/if}
