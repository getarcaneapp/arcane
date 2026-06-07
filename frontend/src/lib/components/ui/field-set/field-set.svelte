<script lang="ts">
	import { cn } from '$lib/utils';
	import { fieldSetVariants } from '.';
	import type { FieldSetRootProps } from './types';

	let { ref = $bindable(null), variant = 'default', children, class: className, ...rest }: FieldSetRootProps = $props();
</script>

<div bind:this={ref} class={cn(fieldSetVariants({ variant }), className)} {...rest}>
	{@render children?.()}
</div>
