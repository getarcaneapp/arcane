<script lang="ts">
	import { cn } from '#lib/utils.js';
	import { LoadingSpinnerIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ComponentProps } from 'svelte';

	type Props = ComponentProps<typeof LoadingSpinnerIcon> & {
		/** Defaults to the surrounding text color. */
		tone?: 'current' | 'foreground' | 'muted' | 'primary' | 'info';
	};

	let { class: className, tone = 'current', ...restProps }: Props = $props();
</script>

<LoadingSpinnerIcon
	role="status"
	aria-label={m.common_loading()}
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
