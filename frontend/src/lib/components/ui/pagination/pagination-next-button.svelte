<script lang="ts">
	import { Pagination as PaginationPrimitive } from 'bits-ui';
	import { ArrowRightIcon } from '$lib/icons';
	import { buttonVariants } from '$lib/components/ui/button/index.js';
	import { cn } from '$lib/utils.js';

	let { ref = $bindable(null), class: className, children, ...restProps }: PaginationPrimitive.NextButtonProps = $props();
</script>

{#snippet Fallback()}
	<ArrowRightIcon class="size-4" />
{/snippet}

<PaginationPrimitive.NextButton
	bind:ref
	class={cn(
		buttonVariants({
			variant: 'ghost',
			class: 'gap-1 pr-2.5'
		}),
		className
	)}
	children={children || Fallback}
	{...restProps}
/>
