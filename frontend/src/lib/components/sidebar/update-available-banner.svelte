<script lang="ts">
	import { cn } from '$lib/utils';

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
		'group flex w-full items-center gap-2.5 text-left transition-colors hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60',
		className
	)}
>
	<span class="relative flex size-2 shrink-0 items-center justify-center">
		<span class="absolute inline-flex size-2 animate-ping rounded-full bg-blue-500 opacity-60"></span>
		<span class="relative inline-flex size-1.5 rounded-full bg-blue-500"></span>
	</span>
	<span class="flex-1 text-sm font-medium text-foreground">
		{label}
	</span>
	{#if versionChip}
		<span class="rounded-md bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
			{versionChip}
		</span>
	{/if}
</button>
