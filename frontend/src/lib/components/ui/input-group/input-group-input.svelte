<script lang="ts">
	import { cn } from '$lib/utils.js';
	import type { HTMLInputAttributes } from 'svelte/elements';

	let { value = $bindable(), class: className, ...props }: HTMLInputAttributes & { value?: string } = $props();
</script>

<input
	data-slot="input-group-control"
	class={cn(
		'selection:bg-primary selection:text-primary-foreground placeholder:text-muted-foreground flex h-full w-full min-w-0 flex-1 bg-transparent px-3 py-1 text-base transition-all outline-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm',
		className
	)}
	bind:value
	{...props}
/>
