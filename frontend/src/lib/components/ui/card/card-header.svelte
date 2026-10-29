<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { cn, type WithElementRef } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		icon,
		iconVariant = 'primary',
		compact = false,
		enableHover = false,
		divider = false,
		loading = false,
		children,
		...restProps
	}: WithElementRef<
		HTMLAttributes<HTMLDivElement> & {
			icon?: any;
			iconVariant?: 'primary' | 'emerald' | 'red' | 'amber' | 'blue' | 'purple' | 'cyan' | 'orange' | 'indigo' | 'pink';
			compact?: boolean;
			enableHover?: boolean;
			/** Rule between the header and the content below. */
			divider?: boolean;
			loading?: boolean;
		}
	> = $props();

	const iconVariantClasses = {
		primary: 'bg-primary/10 text-primary ring-1 ring-primary/20',
		emerald: 'bg-success/10 text-success ring-1 ring-success/20',
		red: 'bg-destructive/10 text-destructive ring-1 ring-destructive/20',
		amber: 'bg-warning/10 text-warning ring-1 ring-warning/20',
		blue: 'bg-info/10 text-info ring-1 ring-info/20',
		purple: 'bg-purple/10 text-purple ring-1 ring-purple/20',
		cyan: 'bg-cyan/10 text-cyan ring-1 ring-cyan/20',
		orange: 'bg-orange/10 text-orange ring-1 ring-orange/20',
		indigo: 'bg-indigo/10 text-indigo ring-1 ring-indigo/20',
		pink: 'bg-pink/10 text-pink ring-1 ring-pink/20'
	};

	const headerPaddingClasses = {
		compact: 'gap-2 p-2',
		icon: 'gap-3 p-4',
		plain: 'py-5'
	};
	const headerVariant = $derived.by((): keyof typeof headerPaddingClasses => {
		if (!icon) return 'plain';
		if (compact) return 'compact';
		return 'icon';
	});
</script>

<div
	bind:this={ref}
	data-slot="card-header"
	data-variant={headerVariant}
	class={cn(
		'@container/card-header relative grid auto-rows-min grid-rows-[auto_auto] items-start gap-1.5 px-6 has-[[data-slot=card-action]]:grid-cols-content-action',
		icon && 'flex flex-row items-center space-y-0',
		headerPaddingClasses[headerVariant],
		divider && 'border-b',
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
