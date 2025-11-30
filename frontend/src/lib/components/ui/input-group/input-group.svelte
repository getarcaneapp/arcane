<script lang="ts">
	import { cn } from '$lib/utils.js';
	import type { HTMLAttributes } from 'svelte/elements';

	let { class: className, children, ...props }: HTMLAttributes<HTMLDivElement> = $props();
</script>

<div
	data-slot="input-group"
	role="group"
	class={cn(
		'group/input-group bg-input/80 relative flex w-full items-center overflow-hidden rounded-lg border shadow-xs backdrop-blur-sm transition-all outline-none',
		'h-9 has-[>textarea]:h-auto',

		// Variants based on alignment.
		'has-[>[data-align=inline-start]]:[&>input]:pl-2',
		'has-[>[data-align=inline-end]]:[&>input]:pr-2',
		'has-[>[data-align=block-start]]:h-auto has-[>[data-align=block-start]]:flex-col has-[>[data-align=block-start]]:[&>input]:pb-3',
		'has-[>[data-align=block-end]]:h-auto has-[>[data-align=block-end]]:flex-col has-[>[data-align=block-end]]:[&>input]:pt-3',

		// Focus state.
		'has-[[data-slot=input-group-control]:focus-visible]:bg-input/90 has-[[data-slot=input-group-control]:focus-visible]:border-ring has-[[data-slot=input-group-control]:focus-visible]:ring-ring/50 has-[[data-slot=input-group-control]:focus-visible]:ring-[3px] has-[[data-slot=input-group-control]:focus-visible]:backdrop-blur-md',

		// Error state.
		'has-[[data-slot][aria-invalid=true]]:ring-destructive/20 has-[[data-slot][aria-invalid=true]]:border-destructive dark:has-[[data-slot][aria-invalid=true]]:ring-destructive/40',

		className
	)}
	{...props}
>
	{@render children?.()}
</div>
