<script lang="ts" module>
	import { cn, type WithElementRef } from '$lib/utils.js';
	import type { HTMLAttributes } from 'svelte/elements';
	import type { ButtonVariant, ButtonSize } from '$lib/components/ui/button/button.svelte';

	export type DropdownButtonProps = WithElementRef<HTMLAttributes<HTMLDivElement>> & {
		variant?: ButtonVariant;
		size?: ButtonSize;
		align?: 'start' | 'center' | 'end';
		disabled?: boolean;
	};
</script>

<script lang="ts">
	import { provideDropdownButtonRoot } from './dropdown-button.svelte.js';

	let {
		class: className,
		ref = $bindable(null),
		children,
		variant = 'default',
		size = 'default',
		align = 'end',
		disabled = false,
		...restProps
	}: DropdownButtonProps = $props();

	$effect(() => {
		provideDropdownButtonRoot({ variant, size, align, disabled });
	});
</script>

<div bind:this={ref} data-slot="dropdown-button" class={cn('flex', className)} {...restProps}>
	{@render children?.()}
</div>
