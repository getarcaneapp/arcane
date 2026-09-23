<script lang="ts">
	import { cn } from '#lib/utils.js';
	import type { HTMLInputAttributes } from 'svelte/elements';

	let {
		value = $bindable(),
		size = 'default',
		mono = false,
		class: className,
		...props
	}: Omit<HTMLInputAttributes, 'size'> & { value?: string; size?: 'default' | 'sm'; mono?: boolean } = $props();
</script>

<input
	data-slot="input-group-control"
	class={cn(
		'flex h-full w-full min-w-0 flex-1 bg-transparent px-3 py-1 text-base transition-all outline-none selection:bg-primary selection:text-primary-foreground placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50 md:text-sm',
		size === 'sm' && 'text-xs md:text-xs',
		mono && 'font-mono',
		className
	)}
	bind:value
	{...props}
/>
