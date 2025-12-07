<script lang="ts">
	import { cn } from '$lib/utils.js';
	import Loader2Icon from 'phosphor-svelte/lib/CircleNotch';
	import type { ComponentProps } from 'svelte';

	type Props = ComponentProps<typeof Loader2Icon>;

	let { class: className, ...restProps }: Props = $props();
</script>

<Loader2Icon role="status" aria-label="Loading" class={cn('size-4 animate-spin', className)} {...restProps} />
