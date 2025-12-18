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
		primary: 'from-primary to-primary/80 shadow-primary/25 border border-primary/20',
		emerald: 'from-emerald-500 to-emerald-600 shadow-emerald-500/25 border border-emerald-400/20',
		red: 'from-red-500 to-red-600 shadow-red-500/25 border border-red-400/20',
		amber: 'from-amber-500 to-amber-600 shadow-amber-500/25 border border-amber-400/20',
		blue: 'from-blue-500 to-blue-600 shadow-blue-500/25 border border-blue-400/20',
		purple: 'from-purple-500 to-purple-600 shadow-purple-500/25 border border-purple-400/20',
		cyan: 'from-cyan-500 to-cyan-600 shadow-cyan-500/25 border border-cyan-400/20',
		orange: 'from-orange-500 to-orange-600 shadow-orange-500/25 border border-orange-400/20',
		indigo: 'from-indigo-500 to-indigo-600 shadow-indigo-500/25 border border-indigo-400/20',
		pink: 'from-pink-500 to-pink-600 shadow-pink-500/25 border border-pink-400/20'
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
		'@container/card-header relative grid auto-rows-min grid-rows-[auto_auto] items-start gap-1.5 px-6 has-[[data-slot=card-action]]:grid-cols-[1fr_auto]',
		'from-muted/60 to-muted/20 dark:from-muted/20 bg-linear-to-b dark:to-transparent',
		icon && 'flex flex-row items-start space-y-0',
		icon && compact ? 'gap-2 p-2' : icon ? 'gap-3 p-4' : 'py-5',
		icon && enableHover && `transition-colors ${headerHoverClass}`,
		className
	)}
	{...restProps}
>
	<!-- Top highlight for edge definition -->
	<div class="pointer-events-none absolute inset-x-0 top-0 h-px bg-white/15 dark:bg-white/5"></div>

	<!-- Subtle inner shadow for depth -->
	<div
		class="pointer-events-none absolute inset-x-0 top-0 h-12 bg-linear-to-b from-black/[0.03] to-transparent dark:from-black/[0.15]"
	></div>

	{#if icon}
		{@const IconComponent = loading ? Spinner : icon}
		<div class="relative shrink-0">
			<div
				class={cn(
					'relative flex items-center justify-center rounded-xl bg-linear-to-br shadow-[0_1px_2px_rgba(0,0,0,0.1),0_4px_12px_-2px_rgba(0,0,0,0.15)] ring-1 ring-black/10 transition-all duration-300 group-[&:not(:has(button:hover,a:hover,[role=button]:hover))]:hover:scale-105 dark:ring-white/20',
					iconVariantClasses[iconVariant],
					compact ? 'size-8 sm:size-10' : 'size-10'
				)}
			>
				<!-- Inner bevel for the icon container -->
				<div class="pointer-events-none absolute inset-0 rounded-xl border-t border-white/20"></div>
				<IconComponent class={cn('text-white drop-shadow-md', compact ? 'size-4 sm:size-5' : 'size-5')} />
			</div>
		</div>
	{/if}
	{@render children?.()}
</div>
