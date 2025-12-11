<script lang="ts" module>
	import { cn, type WithElementRef } from '$lib/utils.js';
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import { buttonVariants, type ButtonVariant, type ButtonSize } from '$lib/components/ui/button/button.svelte';

	export type DropdownButtonTriggerProps = WithElementRef<HTMLButtonAttributes> & {
		variant?: ButtonVariant;
		size?: ButtonSize;
		builders?: any[];
	};
</script>

<script lang="ts">
	import { ArrowDownIcon } from '$lib/icons';
	import { tryUseDropdownButtonRoot } from './dropdown-button.svelte.js';
	const root = tryUseDropdownButtonRoot();

	let {
		class: className,
		variant = root?.variant ?? 'default',
		size = root?.size ?? 'default',
		ref = $bindable(null),
		type = 'button',
		disabled = root?.disabled ?? undefined,
		builders = [],
		children,
		...restProps
	}: DropdownButtonTriggerProps = $props();
</script>

<button
	bind:this={ref}
	use:builders[0]
	data-slot="dropdown-button-trigger"
	class={cn(buttonVariants({ variant, size }), 'border-l-background/20 rounded-l-none border-l px-2', className)}
	{type}
	{disabled}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<ArrowDownIcon class="size-4" />
	{/if}
</button>
