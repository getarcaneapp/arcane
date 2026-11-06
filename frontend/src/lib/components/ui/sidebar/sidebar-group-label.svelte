<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn, type WithElementRef } from '#lib/utils.js';

	import { useSidebar } from './context.svelte.js';

	let {
		ref = $bindable(null),
		children,
		child,
		class: className,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLElement>> & {
		child?: Snippet<[{ props: Record<string, unknown> }]>;
	} = $props();

	const sidebar = useSidebar();

	const mergedProps = $derived({
		class: cn(
			'flex h-10 shrink-0 items-end rounded-md px-2.5 pb-2 text-sm font-semibold text-muted-foreground ring-sidebar-ring outline-hidden transition-[margin,opacity] duration-200 ease-linear focus-visible:ring-2 [&>svg]:size-4 [&>svg]:shrink-0',
			sidebar.hoverExpansionEnabled
				? 'group-data-[collapsible=icon]:-mt-10 group-data-[collapsible=icon]:opacity-0 group-data-[collapsible=icon]:group-data-[hovered=true]:mt-0 group-data-[collapsible=icon]:group-data-[hovered=true]:opacity-100'
				: 'group-data-[collapsible=icon]:hidden',
			className
		),
		'data-slot': 'sidebar-group-label',
		'data-sidebar': 'group-label',
		...restProps
	});
</script>

{#if child}
	{@render child({ props: mergedProps })}
{:else}
	<div bind:this={ref} {...mergedProps}>
		{@render children?.()}
	</div>
{/if}
