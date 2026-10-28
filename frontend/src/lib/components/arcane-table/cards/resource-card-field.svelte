<script lang="ts">
	import type { Snippet } from 'svelte';

	import type { IconType } from '#lib/icons/index.js';
	import { cn } from '#lib/utils.js';

	let {
		label,
		icon: Icon,
		iconClass,
		children
	}: {
		label: string;
		icon?: IconType;
		iconClass?: string;
		children: Snippet;
	} = $props();
</script>

<div class="flex min-h-6 min-w-0 items-start gap-3 text-sm">
	<dt class="flex h-6 w-28 shrink-0 items-center gap-1.5 text-xs text-muted-foreground">
		{#if Icon}
			<Icon class={cn('size-3.5 shrink-0', iconClass ?? 'opacity-70')} />
		{/if}
		<span class="truncate">{label}</span>
	</dt>
	<dd class="flex min-h-6 min-w-0 flex-1 flex-wrap items-center gap-1 overflow-hidden wrap-break-word *:max-w-full *:min-w-0">
		{@render children()}
	</dd>
</div>
