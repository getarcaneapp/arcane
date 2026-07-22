<script lang="ts" module>
	import { type VariantProps, tv } from 'tailwind-variants';

	export const badgeVariants = tv({
		base: 'focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden whitespace-nowrap rounded-lg border text-xs font-medium transition-colors focus-visible:ring-[3px] [&>svg]:pointer-events-none [&>svg]:size-3',
		variants: {
			variant: {
				default: 'bg-primary text-primary-foreground [a&]:hover:bg-primary/90 border-transparent',
				secondary: 'bg-secondary text-secondary-foreground [a&]:hover:bg-secondary/90 border-transparent',
				destructive:
					'bg-destructive [a&]:hover:bg-destructive/90 focus-visible:ring-destructive/20 dark:focus-visible:ring-destructive/40 dark:bg-destructive/70 border-transparent text-white',
				outline:
					'backdrop-blur-sm bg-card/60 text-foreground [a&]:hover:backdrop-blur-sm [a&]:hover:bg-card/90 [a&]:hover:text-accent-foreground',
				primary: 'text-primary bg-primary/10 border-primary/20',
				red: 'text-red-600 bg-red-500/10 border-red-500/20 dark:text-red-400 dark:bg-red-500/10 dark:border-red-500/30',
				orange:
					'text-orange-600 bg-orange-500/10 border-orange-500/20 dark:text-orange-400 dark:bg-orange-500/10 dark:border-orange-500/30',
				amber:
					'text-amber-600 bg-amber-500/10 border-amber-500/20 dark:text-amber-400 dark:bg-amber-500/10 dark:border-amber-500/30',
				lime: 'text-lime-600 bg-lime-500/10 border-lime-500/20 dark:text-lime-400 dark:bg-lime-500/10 dark:border-lime-500/30',
				green:
					'text-emerald-600 bg-emerald-500/10 border-emerald-500/20 dark:text-emerald-400 dark:bg-emerald-500/10 dark:border-emerald-500/30',
				emerald:
					'text-emerald-600 bg-emerald-500/10 border-emerald-500/20 dark:text-emerald-400 dark:bg-emerald-500/10 dark:border-emerald-500/30',
				teal: 'text-teal-600 bg-teal-500/10 border-teal-500/20 dark:text-teal-400 dark:bg-teal-500/10 dark:border-teal-500/30',
				cyan: 'text-cyan-600 bg-cyan-500/10 border-cyan-500/20 dark:text-cyan-400 dark:bg-cyan-500/10 dark:border-cyan-500/30',
				sky: 'text-sky-600 bg-sky-500/10 border-sky-500/20 dark:text-sky-400 dark:bg-sky-500/10 dark:border-sky-500/30',
				blue: 'text-blue-600 bg-blue-500/10 border-blue-500/20 dark:text-blue-400 dark:bg-blue-500/10 dark:border-blue-500/30',
				indigo:
					'text-indigo-600 bg-indigo-500/10 border-indigo-500/20 dark:text-indigo-400 dark:bg-indigo-500/10 dark:border-indigo-500/30',
				violet:
					'text-violet-600 bg-violet-500/10 border-violet-500/20 dark:text-violet-400 dark:bg-violet-500/10 dark:border-violet-500/30',
				purple:
					'text-purple-600 bg-purple-500/10 border-purple-500/20 dark:text-purple-400 dark:bg-purple-500/10 dark:border-purple-500/30',
				fuchsia:
					'text-fuchsia-600 bg-fuchsia-500/10 border-fuchsia-500/20 dark:text-fuchsia-400 dark:bg-fuchsia-500/10 dark:border-fuchsia-500/30',
				pink: 'text-pink-600 bg-pink-500/10 border-pink-500/20 dark:text-pink-400 dark:bg-pink-500/10 dark:border-pink-500/30',
				rose: 'text-rose-600 bg-rose-500/10 border-rose-500/20 dark:text-rose-400 dark:bg-rose-500/10 dark:border-rose-500/30',
				gray: 'text-muted-foreground bg-muted/50 border-border/50 dark:bg-muted/20 dark:border-border/20'
			},
			size: {
				sm: 'px-2 py-0.5 text-[11px] [&>svg]:size-2.5',
				default: 'px-2.5 py-1',
				lg: 'px-3 py-1.5 text-[13px]'
			},
			minWidth: {
				none: '',
				'16': 'min-w-16',
				'20': 'min-w-20',
				'24': 'min-w-24',
				'28': 'min-w-28'
			},
			hoverEffect: {
				none: '',
				lift: '[a&]:hover-lift'
			}
		},
		defaultVariants: {
			variant: 'default',
			size: 'default',
			minWidth: 'none',
			hoverEffect: 'lift'
		}
	});

	export type BadgeVariant = VariantProps<typeof badgeVariants>['variant'];
	export type BadgeSize = VariantProps<typeof badgeVariants>['size'];
	export type BadgeMinWidth = VariantProps<typeof badgeVariants>['minWidth'];
	export type BadgeHoverEffect = VariantProps<typeof badgeVariants>['hoverEffect'];
</script>

<script lang="ts">
	import type { HTMLAnchorAttributes } from 'svelte/elements';
	import { cn, type WithElementRef } from '$lib/utils.js';

	let {
		ref = $bindable(null),
		href,
		class: className,
		variant = 'default',
		size = 'default',
		minWidth = 'none',
		hoverEffect = 'lift',
		children,
		...restProps
	}: WithElementRef<HTMLAnchorAttributes> & {
		variant?: BadgeVariant;
		size?: BadgeSize;
		minWidth?: BadgeMinWidth;
		hoverEffect?: BadgeHoverEffect;
	} = $props();
	void cn;
</script>

<svelte:element
	this={href ? 'a' : 'span'}
	bind:this={ref}
	data-slot="badge"
	{href}
	class={cn(badgeVariants({ variant, size, minWidth, hoverEffect }), className)}
	{...restProps}
>
	{@render children?.()}
</svelte:element>
