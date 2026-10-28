<script lang="ts">
	import type { Snippet } from 'svelte';

	import { cn } from '#lib/utils.js';

	let {
		selected = false,
		onclick,
		leading,
		title,
		actions,
		badges,
		fields,
		footer,
		children,
		class: className
	}: {
		selected?: boolean;
		onclick?: (event: MouseEvent) => void;
		/** Sits before the title, e.g. a selection checkbox. */
		leading?: Snippet;
		title: Snippet;
		actions?: Snippet;
		/** Status-like values shown directly under the title. */
		badges?: Snippet;
		/** `ResourceCardField` children laid out on a two-column grid. */
		fields?: Snippet;
		footer?: Snippet;
		/** Extra content below the fields, e.g. an expanded row. */
		children?: Snippet;
		class?: string;
	} = $props();
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<article
	data-slot="resource-card"
	data-state={selected ? 'selected' : undefined}
	class={cn(
		'group/resource-card relative flex h-full min-w-0 flex-col overflow-hidden rounded-xl border border-border/70 bg-card/80 transition-colors duration-200 hover:border-border',
		selected && 'border-primary/40 bg-primary/5 ring-1 ring-primary/30 hover:border-primary/50',
		onclick && 'cursor-pointer',
		className
	)}
	{onclick}
>
	<header class="flex min-h-14 items-center gap-2.5 px-4 pt-3.5 pb-2">
		{#if leading}
			{@render leading()}
		{/if}
		<div class="min-w-0 flex-1 overflow-hidden text-sm font-medium">
			{@render title()}
		</div>
		{#if actions}
			<div class="-mr-1.5 flex shrink-0 items-center" data-row-select-ignore>
				{@render actions()}
			</div>
		{/if}
	</header>

	{#if badges}
		<div class="flex flex-wrap items-center gap-1.5 px-4 pb-1">
			{@render badges()}
		</div>
	{/if}

	{#if fields}
		<dl class="grid flex-1 grid-cols-2 content-start gap-x-4 gap-y-3 px-4 pt-3 pb-4">
			{@render fields()}
		</dl>
	{:else}
		<div class="flex-1 pb-3"></div>
	{/if}

	{#if children}
		{@render children()}
	{/if}

	{#if footer}
		<footer
			class="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-border/50 bg-muted/40 px-4 py-2 text-xs text-muted-foreground tabular-nums"
		>
			{@render footer()}
		</footer>
	{/if}
</article>
