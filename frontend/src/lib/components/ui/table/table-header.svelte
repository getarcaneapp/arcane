<script lang="ts">
	import { cn, type WithElementRef } from '$lib/utils.js';
	import type { HTMLAttributes } from 'svelte/elements';

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLTableSectionElement>> = $props();
</script>

<thead
	bind:this={ref}
	data-slot="table-header"
	class={cn('bg-primary/10 backdrop-blur-sm [&_tr]:border-b [&_tr]:border-border/50', className)}
	{...restProps}
>
	{@render children?.()}
</thead>
