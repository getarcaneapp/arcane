<script lang="ts">
	import type { Snippet } from 'svelte';

	import type { IconType } from '#lib/icons/index.js';
	import { cn } from '#lib/utils.js';

	let {
		label,
		icon: Icon,
		iconClass,
		layout = 'inline',
		children
	}: {
		label: string;
		icon?: IconType;
		iconClass?: string;
		layout?: 'inline' | 'stacked';
		children: Snippet;
	} = $props();
</script>

<div class={cn('flex min-h-6 min-w-0 text-sm', layout === 'stacked' ? 'flex-col items-stretch gap-1' : 'items-start gap-3')}>
	<dt
		class={cn(
			'flex h-6 shrink-0 items-center gap-1.5 text-xs-plus leading-5 text-muted-foreground',
			layout === 'stacked' ? 'w-auto' : 'w-28'
		)}
	>
		{#if Icon}
			<Icon class={cn('size-3.5 shrink-0', iconClass ?? 'opacity-70')} />
		{/if}
		<span class="truncate">{label}</span>
	</dt>
	<dd
		class={cn(
			'flex min-h-6 min-w-0 flex-wrap items-center gap-1 overflow-hidden wrap-break-word *:max-w-full *:min-w-0',
			layout === 'stacked' ? 'w-full' : 'flex-1'
		)}
	>
		{@render children()}
	</dd>
</div>
