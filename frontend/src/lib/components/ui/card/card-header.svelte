<script lang="ts">
	import { cn, type WithElementRef } from '#lib/utils.js';
	import type { HTMLAttributes } from 'svelte/elements';
	import { Spinner } from '#lib/components/ui/spinner/index.js';

	let {
		ref = $bindable(null),
		class: className,
		icon,
		iconVariant = 'primary',
		compact = false,
		enableHover = false,
		loading = false,
		children,
		...restProps
	}: WithElementRef<
		HTMLAttributes<HTMLDivElement> & {
			icon?: any;
			iconVariant?: 'primary' | 'emerald' | 'red' | 'amber' | 'blue' | 'purple' | 'cyan' | 'orange' | 'indigo' | 'pink';
			compact?: boolean;
			enableHover?: boolean;
			loading?: boolean;
		}
	> = $props();

	const iconVariantClasses = {
		primary: 'bg-primary/10 text-primary ring-1 ring-primary/20',
		emerald: 'bg-emerald-500/10 text-emerald-600 ring-1 ring-emerald-500/20 dark:text-emerald-400',
		red: 'bg-red-500/10 text-red-600 ring-1 ring-red-500/20 dark:text-red-400',
		amber: 'bg-amber-500/10 text-amber-600 ring-1 ring-amber-500/20 dark:text-amber-400',
		blue: 'bg-blue-500/10 text-blue-600 ring-1 ring-blue-500/20 dark:text-blue-400',
		purple: 'bg-purple-500/10 text-purple-600 ring-1 ring-purple-500/20 dark:text-purple-400',
		cyan: 'bg-cyan-500/10 text-cyan-600 ring-1 ring-cyan-500/20 dark:text-cyan-400',
		orange: 'bg-orange-500/10 text-orange-600 ring-1 ring-orange-500/20 dark:text-orange-400',
		indigo: 'bg-indigo-500/10 text-indigo-600 ring-1 ring-indigo-500/20 dark:text-indigo-400',
		pink: 'bg-pink-500/10 text-pink-600 ring-1 ring-pink-500/20 dark:text-pink-400'
	};
</script>

<div
	bind:this={ref}
	data-slot="card-header"
	class={cn(
		'@container/card-header relative grid auto-rows-min grid-rows-[auto_auto] items-start gap-1.5 px-6 has-[[data-slot=card-action]]:grid-cols-[1fr_auto]',
		icon && 'flex flex-row items-start space-y-0',
		icon && compact ? 'gap-2 p-2' : icon ? 'gap-3 p-4' : 'py-5',
		icon && enableHover && 'transition-colors group-[&:not(:has(button:hover,a:hover,[role=button]:hover))]:hover:bg-muted/30',
		className
	)}
	{...restProps}
>
	{#if icon}
		{@const IconComponent = loading ? Spinner : icon}
		<div class="relative shrink-0">
			<div
				class={cn(
					'relative flex items-center justify-center rounded-md transition-colors',
					iconVariantClasses[iconVariant],
					compact ? 'size-8 sm:size-10' : 'size-10'
				)}
			>
				<IconComponent class={cn(compact ? 'size-4 sm:size-5' : 'size-5')} />
			</div>
		</div>
	{/if}
	{@render children?.()}
</div>
