<script lang="ts">
	import { cn } from '#lib/utils.js';

	import type { FieldSetRootProps } from './types';
	import { fieldSetVariants } from './variants';

	let { ref = $bindable(null), variant = 'default', children, class: className, ...rest }: FieldSetRootProps = $props();
</script>

<div bind:this={ref} class={cn(fieldSetVariants({ variant }), className)} {...rest}>
	{@render children?.()}
</div>
