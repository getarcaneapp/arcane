<script lang="ts" module>
	import { type VariantProps, tv } from 'tailwind-variants';

	export const badgeVariants = tv({
		base: 'focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden whitespace-nowrap rounded-lg border text-xs font-medium transition-colors focus-visible:ring-3 [&>svg]:pointer-events-none [&>svg]:size-3',
		variants: {
			variant: {
				default: 'bg-primary text-primary-foreground [a&]:hover:bg-primary/90 border-transparent',
				secondary: 'bg-secondary text-secondary-foreground [a&]:hover:bg-secondary/90 border-transparent',
				destructive:
					'bg-destructive [a&]:hover:bg-destructive/90 focus-visible:ring-destructive/20 dark:focus-visible:ring-destructive/40 dark:bg-destructive/70 border-transparent text-white',
				outline:
					'backdrop-blur-sm bg-card/60 text-foreground [a&]:hover:backdrop-blur-sm [a&]:hover:bg-card/90 [a&]:hover:text-accent-foreground',
				primary: 'text-primary bg-primary/10 border-primary/20 dark:text-primary-tint dark:bg-primary/15 dark:border-primary/30',
				red: 'text-destructive bg-destructive/10 border-destructive/20 dark:border-destructive/30',
				orange: 'text-orange bg-orange/10 border-orange/20 dark:border-orange/30',
				amber: 'text-warning bg-warning/10 border-warning/20 dark:border-warning/30',
				lime: 'text-lime bg-lime/10 border-lime/20 dark:border-lime/30',
				green: 'text-success bg-success/10 border-success/20 dark:border-success/30',
				emerald: 'text-success bg-success/10 border-success/20 dark:border-success/30',
				teal: 'text-teal bg-teal/10 border-teal/20 dark:border-teal/30',
				cyan: 'text-cyan bg-cyan/10 border-cyan/20 dark:border-cyan/30',
				sky: 'text-sky bg-sky/10 border-sky/20 dark:border-sky/30',
				blue: 'text-info bg-info/10 border-info/20 dark:border-info/30',
				indigo: 'text-indigo bg-indigo/10 border-indigo/20 dark:border-indigo/30',
				violet: 'text-violet bg-violet/10 border-violet/20 dark:border-violet/30',
				purple: 'text-purple bg-purple/10 border-purple/20 dark:border-purple/30',
				fuchsia: 'text-fuchsia bg-fuchsia/10 border-fuchsia/20 dark:border-fuchsia/30',
				pink: 'text-pink bg-pink/10 border-pink/20 dark:border-pink/30',
				rose: 'text-rose bg-rose/10 border-rose/20 dark:border-rose/30',
				gray: 'text-muted-foreground bg-muted/50 border-border/50 dark:bg-muted/20 dark:border-border/20'
			},
			size: {
				xs: 'px-1.5 py-0 text-3xs leading-4 [&>svg]:size-2.5',
				sm: 'px-2 py-0.5 text-2xs [&>svg]:size-2.5',
				default: 'px-2.5 py-1',
				lg: 'px-3 py-1.5 text-xs-plus'
			},
			mono: {
				true: 'font-mono',
				false: ''
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
			hoverEffect: 'lift',
			mono: false
		}
	});

	export type BadgeVariant = VariantProps<typeof badgeVariants>['variant'];
	export type BadgeSize = VariantProps<typeof badgeVariants>['size'];
	export type BadgeMinWidth = VariantProps<typeof badgeVariants>['minWidth'];
	export type BadgeHoverEffect = VariantProps<typeof badgeVariants>['hoverEffect'];
</script>

<script lang="ts">
	import type { HTMLAnchorAttributes } from 'svelte/elements';
	import { cn, type WithElementRef } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		href,
		class: className,
		variant = 'default',
		size = 'default',
		minWidth = 'none',
		hoverEffect = 'lift',
		mono = false,
		children,
		...restProps
	}: WithElementRef<HTMLAnchorAttributes> & {
		variant?: BadgeVariant;
		size?: BadgeSize;
		minWidth?: BadgeMinWidth;
		hoverEffect?: BadgeHoverEffect;
		/** Monospace text for code-like values. */
		mono?: boolean;
	} = $props();
	void cn;
</script>

<svelte:element
	this={href ? 'a' : 'span'}
	bind:this={ref}
	data-slot="badge"
	{href}
	class={cn(badgeVariants({ variant, size, minWidth, hoverEffect, mono }), className)}
	{...restProps}
>
	{@render children?.()}
</svelte:element>
