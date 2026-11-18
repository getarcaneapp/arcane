<script lang="ts">
	import { cn, type WithElementRef } from '@arcane/ui/utils';
	import type { HTMLAttributes } from 'svelte/elements';

	let { ref = $bindable(null), class: className, children, ...restProps }: WithElementRef<HTMLAttributes<HTMLElement>> = $props();
</script>

<div
	bind:this={ref}
	data-slot="sidebar-group"
	data-sidebar="group"
	class={cn('relative flex w-full min-w-0 shrink-0 flex-col', className)}
	{...restProps}
>
	{@render children?.()}
</div>
