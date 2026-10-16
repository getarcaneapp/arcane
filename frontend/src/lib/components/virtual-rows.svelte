<script lang="ts" generics="TRow">
	import type { Snippet } from 'svelte';

	import type { createVirtualizer } from '#lib/components/ui/virtualizer.svelte.js';
	import { cn } from '#lib/utils.js';

	let {
		virtualizer,
		rows,
		class: className,
		row
	}: {
		virtualizer: ReturnType<typeof createVirtualizer<HTMLElement, HTMLDivElement>>;
		rows: TRow[];
		class?: string;
		row: Snippet<[TRow, number]>;
	} = $props();
</script>

<div class={cn('relative h-(--total-height)', className)} style={`--total-height: ${virtualizer.totalSize}px`}>
	{#each virtualizer.virtualItems as virtualItem (virtualItem.key)}
		{@const item = rows[virtualItem.index]}
		{#if item}
			<div
				class="absolute top-0 left-0 w-full translate-y-(--row-start)"
				style={`--row-start: ${virtualItem.start}px`}
				data-index={virtualItem.index}
				{@attach virtualizer.measureElement}
			>
				{@render row(item, virtualItem.index)}
			</div>
		{/if}
	{/each}
</div>
