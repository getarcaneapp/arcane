<script lang="ts">
	import type { ComponentProps } from 'svelte';
	import LoadingSpinnerIcon from 'virtual:icons/svg-spinners/ring-resize';

	import { getUiLabels } from '#lib/labels.js';
	import { cn } from '#lib/utils.js';

	type Props = ComponentProps<typeof LoadingSpinnerIcon> & {
		/** Defaults to the surrounding text color. */
		tone?: 'current' | 'foreground' | 'muted' | 'primary' | 'info';
	};

	let { class: className, tone = 'current', ...restProps }: Props = $props();

	const labels = getUiLabels();
</script>

<LoadingSpinnerIcon
	role="status"
	aria-label={labels.loading()}
	class={cn(
		'size-4',
		tone === 'foreground' && 'text-foreground',
		tone === 'muted' && 'text-muted-foreground',
		tone === 'primary' && 'text-primary',
		tone === 'info' && 'text-info',
		className
	)}
	{...restProps}
/>
