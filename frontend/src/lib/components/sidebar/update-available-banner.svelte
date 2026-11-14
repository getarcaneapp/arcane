<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';

	let {
		label,
		versionChip,
		updateCount = 0,
		disabled = false,
		class: className,
		onclick
	}: {
		label: string;
		versionChip?: string | null;
		updateCount?: number;
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
		'relative flex w-full items-center gap-2.5 overflow-hidden rounded-xl border border-primary/25 bg-primary/5 px-2.5 py-2 text-left transition-colors hover:border-primary/40 hover:bg-primary/10 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden disabled:cursor-not-allowed disabled:opacity-60',
		className
	)}
>
	<span class="grid min-w-0 flex-1 leading-tight">
		<span class="truncate text-xs font-medium">{label}</span>
		{#if updateCount > 0}
			<span class="truncate text-2xs text-muted-foreground tabular-nums">{m.count_to_update({ count: updateCount })}</span>
		{/if}
	</span>
	{#if versionChip}
		<span class="max-w-28 shrink-0 truncate font-mono text-xs font-medium text-foreground tabular-nums">{versionChip}</span>
	{/if}
</button>

<style>
	button::after {
		content: '';
		position: absolute;
		inset: 0;
		pointer-events: none;
		background: linear-gradient(
			105deg,
			transparent 35%,
			color-mix(in oklab, var(--primary) 28%, transparent) 50%,
			transparent 65%
		);
		transform: translateX(-100%);
	}
	button:hover:not(:disabled)::after {
		transform: translateX(100%);
		transition: transform 800ms ease-out;
	}
	@media (prefers-reduced-motion: reduce) {
		button::after {
			display: none;
		}
	}
</style>
