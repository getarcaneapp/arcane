<script lang="ts">
	import { Checkbox as CheckboxPrimitive } from 'bits-ui';
	import { cn, type WithoutChildrenOrChild } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		checked = $bindable(false),
		indeterminate = $bindable(false),
		class: className,
		...restProps
	}: WithoutChildrenOrChild<CheckboxPrimitive.RootProps> = $props();
</script>

<CheckboxPrimitive.Root
	bind:ref
	data-slot="checkbox"
	class={cn(
		'peer relative flex size-4 shrink-0 items-center justify-center rounded-[4px] border border-input bg-input shadow-xs transition-shadow outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 data-[state=checked]:border-primary data-[state=checked]:bg-primary data-[state=checked]:text-primary-foreground data-[state=indeterminate]:border-primary data-[state=indeterminate]:bg-primary data-[state=indeterminate]:text-primary-foreground dark:aria-invalid:ring-destructive/40',
		className
	)}
	bind:checked
	bind:indeterminate
	{...restProps}
>
	{#snippet children({ checked, indeterminate })}
		<div
			data-slot="checkbox-indicator"
			class="pointer-events-none absolute inset-0 flex items-center justify-center text-current transition-none"
		>
			{#if checked}
				<svg
					class="absolute top-1/2 left-1/2 size-3 -translate-x-1/2 -translate-y-1/2"
					viewBox="0 0 12 12"
					fill="none"
					aria-hidden="true"
				>
					<path
						d="M3 6.15L5.2 8.25L9 3.85"
						stroke="currentColor"
						stroke-width="1.8"
						stroke-linecap="round"
						stroke-linejoin="round"
					/>
				</svg>
			{:else if indeterminate}
				<span class="absolute top-1/2 left-1/2 h-[2px] w-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-current"></span>
			{/if}
		</div>
	{/snippet}
</CheckboxPrimitive.Root>
