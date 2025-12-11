<script lang="ts">
	import { cn } from '$lib/utils.js';
	import { LoadingSpinnerIcon } from '$lib/icons';
	import type { ComponentProps } from 'svelte';

	type Props = ComponentProps<typeof LoadingSpinnerIcon>;

	let { class: className, ...restProps }: Props = $props();
</script>

<LoadingSpinnerIcon role="status" aria-label="Loading" class={cn('size-4', className)} {...restProps} />
