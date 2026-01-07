<script lang="ts">
	import { Progress } from '$lib/components/ui/progress/index.js';
	import { m } from '$lib/paraglide/messages';

	interface Props {
		value?: number;
		loading?: boolean;
		stopped?: boolean;
		type: 'cpu' | 'memory';
	}

	let { value, loading = false, stopped = false, type }: Props = $props();
</script>

{#if stopped}
	<div class="text-muted-foreground text-xs">{m.common_na()}</div>
{:else if loading}
	<div class="flex items-center gap-2">
		<div class="bg-muted h-1.5 flex-1 animate-pulse rounded-full"></div>
		<div class="bg-muted h-3 w-10 animate-pulse rounded"></div>
	</div>
{:else if value !== undefined}
	<div class="flex items-center gap-2">
		<Progress value={value} max={100} class="h-1.5 flex-1" />
		<span class="text-foreground min-w-10 text-right text-xs font-medium tabular-nums">
			{value.toFixed(1)}%
		</span>
	</div>
{:else}
	<div class="text-muted-foreground text-xs">—</div>
{/if}

