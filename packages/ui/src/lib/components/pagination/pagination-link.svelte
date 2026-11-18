<script lang="ts">
	import { Pagination as PaginationPrimitive } from 'bits-ui';

	import { type Props, buttonVariants } from '#lib/components/button/index.js';
	import { cn } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		size = 'sm',
		isActive = false,
		page,
		children,
		...restProps
	}: PaginationPrimitive.PageProps &
		Props & {
			isActive: boolean;
		} = $props();
</script>

{#snippet Fallback()}
	{page.value}
{/snippet}

<PaginationPrimitive.Page
	bind:ref
	{page}
	class={cn(
		buttonVariants({
			variant: isActive ? 'outline' : 'ghost',
			size
		}),
		className
	)}
	children={children || Fallback}
	{...restProps}
/>
