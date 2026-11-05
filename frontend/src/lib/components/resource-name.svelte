<script lang="ts">
	import type { Snippet } from 'svelte';

	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { cn } from '#lib/utils.js';

	const maxWidthClasses = {
		'resource-name': 'max-w-resource-name',
		full: 'max-w-full'
	};

	let {
		name,
		href,
		maxWidth = 'resource-name',
		class: className,
		tooltip
	}: {
		name: string;
		href?: string;
		maxWidth?: keyof typeof maxWidthClasses;
		class?: string;
		tooltip?: Snippet;
	} = $props();
</script>

<ArcaneTooltip.Root interactive={!!href}>
	<ArcaneTooltip.Trigger>
		{#snippet child({ props })}
			<svelte:element
				this={href ? 'a' : 'span'}
				{...props}
				{href}
				aria-label={href ? name : undefined}
				class={cn('inline-flex min-w-0 whitespace-nowrap', maxWidthClasses[maxWidth], href && 'hover:underline', className)}
			>
				<span class="truncate">{name.slice(0, -12)}</span><span class="shrink-0">{name.slice(-12)}</span>
			</svelte:element>
		{/snippet}
	</ArcaneTooltip.Trigger>
	<ArcaneTooltip.Content>
		{#if tooltip}
			{@render tooltip()}
		{:else}
			<span class="block max-w-tooltip break-all">{name}</span>
		{/if}
	</ArcaneTooltip.Content>
</ArcaneTooltip.Root>
