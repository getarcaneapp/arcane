<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn, type WithElementRef } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLTableSectionElement>> = $props();
</script>

<thead bind:this={ref} data-slot="table-header" class={cn('[&_tr]:cursor-default [&_tr]:border-b-0', className)} {...restProps}>
	{@render children?.()}
</thead>
