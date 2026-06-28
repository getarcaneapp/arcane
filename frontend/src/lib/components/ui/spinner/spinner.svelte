<script lang="ts">
	import { cn } from '$lib/utils.js';
	import { LoadingSpinnerIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import type { ComponentProps } from 'svelte';

	type Props = ComponentProps<typeof LoadingSpinnerIcon>;

	let { class: className, ...restProps }: Props = $props();
</script>

<LoadingSpinnerIcon role="status" aria-label={m.common_loading()} class={cn('size-4', className)} {...restProps} />
