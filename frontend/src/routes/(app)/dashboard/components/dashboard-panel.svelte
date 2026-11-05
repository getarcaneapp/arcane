<script lang="ts">
	import type { Snippet } from 'svelte';

	import * as Card from '#lib/components/ui/card/index.js';
	import { cn } from '#lib/utils.js';
	import { trackOverflow } from '#lib/utils/overflow.js';

	let {
		title,
		actions,
		footer,
		children
	}: {
		title: string;
		actions?: Snippet;
		footer?: Snippet;
		children: Snippet;
	} = $props();

	let moreBelow = $state(false);
	let scrollable = $state(false);
</script>

<Card.Root role="region" aria-label={title} class="h-full min-w-0">
	<div class="flex h-full flex-col gap-3 p-5">
		<div class="flex min-h-7 flex-wrap items-center justify-between gap-2">
			<h2 class="truncate text-base font-semibold">{title}</h2>
			{@render actions?.()}
		</div>
		<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
		<div
			class={cn('-mx-2 min-h-0 flex-1 scrollbar-visible overflow-y-auto px-2', moreBelow && 'scroll-fade-b')}
			tabindex={scrollable ? 0 : undefined}
			{@attach trackOverflow((overflow) => {
				moreBelow = overflow.below;
				scrollable = overflow.scrollable;
			})}
		>
			{@render children()}
		</div>
		{#if footer}
			<div class="flex shrink-0 items-center">{@render footer()}</div>
		{/if}
	</div>
</Card.Root>
