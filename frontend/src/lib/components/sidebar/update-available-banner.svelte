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
		'group hover:bg-muted/50 focus-visible:ring-primary/40 flex w-full items-center gap-2.5 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60',
		className
	)}
>
	<span class="relative flex size-2 shrink-0 items-center justify-center">
		<span class="absolute inline-flex size-2 animate-ping rounded-full bg-blue-500 opacity-60"></span>
		<span class="relative inline-flex size-1.5 rounded-full bg-blue-500"></span>
	</span>
	<span class="text-foreground flex-1 text-sm font-medium">
		{label}
	</span>
	{#if versionChip}
		<span class="bg-muted text-muted-foreground rounded-md px-1.5 py-0.5 font-mono text-[11px]">
			{versionChip}
		</span>
	{/if}
</button>
