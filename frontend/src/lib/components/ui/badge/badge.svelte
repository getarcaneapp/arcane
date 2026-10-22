<script lang="ts" module>
	import { type VariantProps, tv } from 'tailwind-variants';

	export const badgeVariants = tv({
		base: 'focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden whitespace-nowrap rounded-full border text-xs font-medium transition-colors focus-visible:ring-3 [&>svg]:pointer-events-none [&>svg]:size-3',
		variants: {
			variant: {
				default: 'bg-primary text-primary-foreground [a&]:hover:bg-primary/90 border-transparent',
				secondary: 'bg-secondary text-secondary-foreground [a&]:hover:bg-secondary/90 border-transparent',
				destructive:
					'bg-destructive [a&]:hover:bg-destructive/90 focus-visible:ring-destructive/20 dark:focus-visible:ring-destructive/40 dark:bg-destructive/70 border-transparent text-white',
				outline:
					'backdrop-blur-sm bg-card/60 text-foreground [a&]:hover:backdrop-blur-sm [a&]:hover:bg-card/90 [a&]:hover:text-accent-foreground',
				primary: 'text-primary bg-primary/15 border-primary/80 backdrop-blur-sm dark:text-primary-tint',
				red: 'text-destructive bg-destructive/15 border-destructive/80 backdrop-blur-sm',
				orange: 'text-orange bg-orange/15 border-orange/80 backdrop-blur-sm',
				amber: 'text-warning bg-warning/15 border-warning/80 backdrop-blur-sm',
				lime: 'text-lime bg-lime/15 border-lime/80 backdrop-blur-sm',
				green: 'text-success bg-success/15 border-success/80 backdrop-blur-sm',
				emerald: 'text-success bg-success/15 border-success/80 backdrop-blur-sm',
				teal: 'text-teal bg-teal/15 border-teal/80 backdrop-blur-sm',
				cyan: 'text-cyan bg-cyan/15 border-cyan/80 backdrop-blur-sm',
				sky: 'text-sky bg-sky/15 border-sky/80 backdrop-blur-sm',
				blue: 'text-info bg-info/15 border-info/80 backdrop-blur-sm',
				indigo: 'text-indigo bg-indigo/15 border-indigo/80 backdrop-blur-sm',
				violet: 'text-violet bg-violet/15 border-violet/80 backdrop-blur-sm',
				purple: 'text-purple bg-purple/15 border-purple/80 backdrop-blur-sm',
				fuchsia: 'text-fuchsia bg-fuchsia/15 border-fuchsia/80 backdrop-blur-sm',
				pink: 'text-pink bg-pink/15 border-pink/80 backdrop-blur-sm',
				rose: 'text-rose bg-rose/15 border-rose/80 backdrop-blur-sm',
				gray: 'text-muted-foreground bg-muted-foreground/10 border-muted-foreground/50 backdrop-blur-sm'
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
