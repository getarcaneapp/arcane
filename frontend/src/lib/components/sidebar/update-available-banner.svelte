<script lang="ts">
	import { cn } from '#lib/utils.js';
	import { ArrowRightIcon, CircleArrowUpIcon } from '#lib/icons/index.js';

	let {
		label,
		versionChip,
		disabled = false,
		class: className,
		onclick
	}: {
		label: string;
		versionChip?: string | null;
		disabled?: boolean;
		class?: string;
		onclick?: () => void;
	} = $props();
</script>

<button
	type="button"
	{onclick}
	{disabled}
	class={cn(
		'group flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden disabled:cursor-not-allowed disabled:opacity-60',
		className
	)}
>
	<span class="flex size-6 shrink-0 items-center justify-center rounded-md bg-primary/15 text-primary">
		<CircleArrowUpIcon class="size-3.5 shrink-0" aria-hidden="true" />
	</span>
	<span class="grid min-w-0 flex-1 leading-tight">
		<span class="truncate text-xs font-medium">{label}</span>
		{#if versionChip}
			<span class="truncate text-2xs text-muted-foreground tabular-nums">{versionChip}</span>
		{/if}
	</span>
	<ArrowRightIcon
		class="size-3.5 shrink-0 text-muted-foreground/50 transition-colors group-hover:text-foreground"
		aria-hidden="true"
	/>
</button>
