<script lang="ts">
	import { Badge } from '#lib/components/ui/badge/index.js';

	let { labels, limit = 2 }: { labels: Record<string, string> | null | undefined; limit?: number } = $props();

	const entries = $derived(Object.entries(labels ?? {}));
</script>

{#if entries.length === 0}
	<span class="text-sm text-muted-foreground">-</span>
{:else}
	<div class="flex flex-wrap items-center gap-1.5">
		{#each entries.slice(0, limit) as [key, value] (key)}
			<Badge variant="gray" mono class="max-w-50" title={`${key}=${value}`}>
				<span class="block min-w-0 truncate">{value ? `${key}=${value}` : key}</span>
			</Badge>
		{/each}
		{#if entries.length > limit}
			<Badge variant="outline">{`+${entries.length - limit}`}</Badge>
		{/if}
	</div>
{/if}
