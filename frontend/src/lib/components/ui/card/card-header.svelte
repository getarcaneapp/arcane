<script lang="ts">
	import { cn, type WithElementRef } from '$lib/utils.js';
	import type { HTMLAttributes } from 'svelte/elements';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { mode } from 'mode-watcher';

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
		primary: 'from-primary to-primary/80 shadow-primary/25',
		emerald: 'from-emerald-500 to-emerald-600 shadow-emerald-500/25',
		red: 'from-red-500 to-red-600 shadow-red-500/25',
		amber: 'from-amber-500 to-amber-600 shadow-amber-500/25',
		blue: 'from-blue-500 to-blue-600 shadow-blue-500/25',
		purple: 'from-purple-500 to-purple-600 shadow-purple-500/25',
		cyan: 'from-cyan-500 to-cyan-600 shadow-cyan-500/25',
		orange: 'from-orange-500 to-orange-600 shadow-orange-500/25',
		indigo: 'from-indigo-500 to-indigo-600 shadow-indigo-500/25',
		pink: 'from-pink-500 to-pink-600 shadow-pink-500/25'
	};

	const isDarkMode = $derived(mode.current === 'dark');

	const headerHoverClass = $derived(
		isDarkMode
			? 'group-[&:not(:has(button:hover,a:hover,[role=button]:hover))]:hover:from-primary/8 group-[&:not(:has(button:hover,a:hover,[role=button]:hover))]:hover:to-primary/2'
			: 'group-[&:not(:has(button:hover,a:hover,[role=button]:hover))]:hover:from-primary/6 group-[&:not(:has(button:hover,a:hover,[role=button]:hover))]:hover:to-primary/2'
	);
</script>

<div
	bind:this={ref}
	data-slot="card-header"
	class={cn(
		'@container/card-header grid auto-rows-min grid-rows-[auto_auto] items-start gap-1.5 px-6 has-[[data-slot=card-action]]:grid-cols-[1fr_auto]',
		icon && 'from-primary/3 flex flex-row items-start space-y-0 bg-linear-to-br to-transparent',
		icon && compact ? 'gap-2 p-2' : icon ? 'gap-3 p-3.5' : '',
		icon && enableHover && `transition-colors ${headerHoverClass}`,
		className
	)}
	{...restProps}
>
	{#if icon}
		{@const IconComponent = loading ? Spinner : icon}
		<div
			class={cn(
				'flex shrink-0 items-center justify-center rounded-full bg-linear-to-br shadow-lg transition-transform group-[&:not(:has(button:hover,a:hover,[role=button]:hover))]:hover:scale-105',
				iconVariantClasses[iconVariant],
				compact ? 'size-8 sm:size-10' : 'size-10'
			)}
		>
			<IconComponent class={cn('text-white', compact ? 'size-4 sm:size-5' : 'size-5')} />
		</div>
	{/if}
	{@render children?.()}
</div>
