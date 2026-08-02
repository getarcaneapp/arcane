<script lang="ts">
	import { cn } from '#lib/utils';
	import type { FieldSetFooterProps } from './types';

	let { ref = $bindable(null), class: className, children, ...rest }: FieldSetFooterProps = $props();
</script>

<div bind:this={ref} class={cn('border-t border-inherit px-6 py-3', className)} {...rest}>
	{@render children?.()}
</div>
