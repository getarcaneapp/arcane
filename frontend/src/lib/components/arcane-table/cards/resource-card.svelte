<script lang="ts">
	import type { Snippet } from 'svelte';

	import * as Card from '#lib/components/ui/card/index.js';
	import { cn } from '#lib/utils.js';

	let {
		selected = false,
		onclick,
		onmousedown,
		icon,
		title,
		context,
		actions,
		badges,
		meters,
		fields,
		footer,
		children,
		class: className
	}: {
		selected?: boolean;
		onclick?: (event: MouseEvent) => void;
		onmousedown?: (event: MouseEvent) => void;
		icon?: Snippet;
		title: Snippet;
		context?: Snippet;
		actions?: Snippet;
		badges?: Snippet;
		meters?: Snippet;
		fields?: Snippet;
		footer?: Snippet;
		children?: Snippet;
		class?: string;
	} = $props();
</script>

<Card.Root
	variant="transparent"
	interactive="subtle"
	{selected}
	{onclick}
	{onmousedown}
	data-slot="resource-card"
	data-state={selected ? 'selected' : undefined}
	class={cn('flex h-full min-w-0 flex-col', className)}
>
	<div class="flex min-w-0 flex-1 flex-col divide-y divide-border/40 px-3.5">
		<header class="flex min-w-0 items-start gap-3 py-3">
			{#if icon}
				<div class="shrink-0">
					{@render icon()}
				</div>
			{/if}
			<div class="flex min-w-0 flex-1 flex-col gap-0.5">
				<div class="min-w-0 truncate text-sm font-medium">
					{@render title()}
				</div>
				{#if context}
					<div class="flex min-w-0 items-center gap-1.5 truncate text-xs text-muted-foreground **:text-xs">
						{@render context()}
					</div>
				{/if}
			</div>
			{#if badges || actions}
				<div class="-mr-1.5 flex shrink-0 items-center gap-1.5" data-row-select-ignore>
					{@render badges?.()}
					{@render actions?.()}
				</div>
			{/if}
		</header>

		{#if meters}
			<div class="grid grid-cols-2 gap-4 py-3">
				{@render meters()}
			</div>
		{/if}

		{#if fields}
			<dl class="flex flex-col gap-1.5 py-3">
				{@render fields()}
			</dl>
		{/if}

		{@render children?.()}

		{#if footer}
			<footer
				class="mt-auto grid auto-cols-fr grid-flow-col items-center gap-x-4 py-2.5 text-xs text-muted-foreground tabular-nums"
			>
				{@render footer()}
			</footer>
		{/if}
	</div>
</Card.Root>
