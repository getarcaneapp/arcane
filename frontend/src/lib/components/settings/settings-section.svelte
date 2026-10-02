<script lang="ts">
	import { cn } from '#lib/utils.js';
	import type { Snippet } from 'svelte';

	// A titled group of settings. `list` frames the rows in one bordered, divided block;
	// `plain` leaves the content unframed for tables and card grids.
	interface Props {
		title?: string;
		description?: string;
		variant?: 'list' | 'plain';
		id?: string;
		actions?: Snippet;
		class?: string;
		children: Snippet;
	}

	let { title, description, variant = 'list', id, actions, class: className, children }: Props = $props();
</script>

<section {id} class={cn('flex flex-col gap-3', className)}>
	{#if title || actions}
		<div class="flex flex-wrap items-start justify-between gap-x-6 gap-y-2 px-1">
			<div class="min-w-0">
				{#if title}
					<h2 class="text-sm font-semibold">{title}</h2>
				{/if}
				{#if description}
					<p class="text-xs text-muted-foreground">{description}</p>
				{/if}
			</div>
			{#if actions}
				<div class="flex shrink-0 items-center gap-2">{@render actions()}</div>
			{/if}
		</div>
	{/if}
	{#if variant === 'list'}
		<div class="divide-y divide-border/50 overflow-hidden rounded-xl border border-border/60 bg-card/30">
			{@render children()}
		</div>
	{:else}
		{@render children()}
	{/if}
</section>
