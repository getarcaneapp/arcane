<script lang="ts" module>
	import type { IconType } from '#lib/icons/index.js';

	export interface DetailMetaItem {
		icon?: IconType;
		label?: string;
		value: string;
		mono?: boolean;
	}
</script>

<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cn } from '#lib/utils.js';

	interface Props {
		items?: DetailMetaItem[];
		class?: string;
		children?: Snippet;
	}

	let { items = [], class: className, children }: Props = $props();
</script>

<div class={cn('flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg bg-muted/40 px-4 py-3', className)}>
	<!-- Keyed by index on purpose: rows are stateless (icon + text from props) and
	     labels are not guaranteed unique, so a label-based key could collide. -->
	{#each items as item, i (i)}
		<div class="flex items-center gap-1.5 text-sm text-muted-foreground">
			{#if item.icon}
				<item.icon class="size-4 shrink-0" />
			{/if}
			{#if item.label}
				<span class="text-xs font-semibold tracking-wide uppercase">{item.label}</span>
			{/if}
			<span class={cn(item.mono && 'font-mono break-all select-all')}>{item.value}</span>
		</div>
	{/each}
	{@render children?.()}
</div>
