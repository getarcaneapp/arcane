<script lang="ts">
	import type { Snippet } from 'svelte';

	import { cn } from '#lib/utils.js';

	let {
		selected = false,
		onclick,
		onmousedown,
		title,
		context,
		actions,
		badges,
		fields,
		footer,
		children,
		class: className
	}: {
		selected?: boolean;
		onclick?: (event: MouseEvent) => void;
		onmousedown?: (event: MouseEvent) => void;
		title: Snippet;
		/** Secondary line under the title, e.g. the environment. */
		context?: Snippet;
		actions?: Snippet;
		/** Status pills and indicators, the same ones the list row shows. */
		badges?: Snippet;
		/** `ResourceCardField` rows. */
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
		'group/resource-card relative flex h-full min-w-0 flex-col rounded-lg border border-border/50 bg-card/30 transition-colors duration-150 hover:border-border hover:bg-muted/30',
		selected && 'border-primary/40 bg-primary/5 hover:border-primary/50 hover:bg-primary/8',
		onclick && 'cursor-pointer',
		className
	)}
	{onclick}
	{onmousedown}
>
	<header class="flex items-start gap-2 px-3.5 pt-3">
		<div class="flex min-w-0 flex-1 flex-col gap-0.5">
			<div class="min-w-0 overflow-hidden text-sm leading-5 font-medium">
				{@render title()}
			</div>
			{#if context}
				<div class="min-w-0 overflow-hidden text-xs *:text-xs">
					{@render context()}
				</div>
			{/if}
		</div>
		{#if badges}
			<div class="flex max-w-1/2 shrink-0 flex-wrap items-center justify-end gap-1.5">
				{@render badges()}
			</div>
		{/if}
		{#if actions}
			<div class="-mt-1 -mr-1.5 flex shrink-0 items-center" data-row-select-ignore>
				{@render actions()}
			</div>
		{/if}
	</header>

	<dl class="flex flex-1 flex-col gap-1 px-3.5 pt-2.5 pb-3">
		{@render fields?.()}
	</dl>

	{@render children?.()}

	{#if footer}
		<footer
			class="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-border/40 px-3.5 py-2 text-xs text-muted-foreground tabular-nums"
		>
			{@render footer()}
		</footer>
	{/if}
</article>
