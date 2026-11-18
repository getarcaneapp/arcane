<script lang="ts">
	import { Pagination as PaginationPrimitive } from 'bits-ui';
	import ArrowLeftIcon from 'virtual:icons/solar/alt-arrow-left-bold';

	import { buttonVariants } from '#lib/components/button/index.js';
	import { cn } from '#lib/utils.js';

	let { ref = $bindable(null), class: className, children, ...restProps }: PaginationPrimitive.PrevButtonProps = $props();
</script>

{#snippet Fallback()}
	<ArrowLeftIcon class="size-4" />
{/snippet}

<PaginationPrimitive.PrevButton
	bind:ref
	class={cn(
		buttonVariants({
			variant: 'ghost',
			class: 'gap-1 pl-2.5'
		}),
		className
	)}
	children={children || Fallback}
	{...restProps}
/>
