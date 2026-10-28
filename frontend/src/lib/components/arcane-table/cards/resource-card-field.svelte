<script lang="ts">
	import type { Snippet } from 'svelte';

	import type { IconType } from '#lib/icons/index.js';
	import { cn } from '#lib/utils.js';

	let {
		label,
		icon: Icon,
		iconClass,
		wide = false,
		children
	}: {
		label: string;
		icon?: IconType;
		iconClass?: string;
		/** Spans both grid columns, for long or multi-value content. */
		wide?: boolean;
		children: Snippet;
	} = $props();
</script>

<div class={cn('flex min-w-0 flex-col gap-1', wide && 'col-span-2')}>
	<dt class="flex items-center gap-1.5 text-2xs font-medium tracking-wide text-muted-foreground uppercase">
		{#if Icon}
			<Icon class={cn('size-3 shrink-0', iconClass ?? 'opacity-70')} />
		{/if}
		<span class="truncate">{label}</span>
	</dt>
	<dd class="flex min-h-6 min-w-0 flex-wrap items-center gap-1 text-sm wrap-break-word">
		{@render children()}
	</dd>
</div>
