<script lang="ts" module>
	import { type VariantProps, tv } from 'tailwind-variants';

	export const badgeVariants = tv({
		base: 'focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden whitespace-nowrap rounded-xl border text-xs font-medium transition-colors focus-visible:ring-3 [&>svg]:pointer-events-none [&>svg]:size-3',
		variants: {
			variant: {
				default: 'text-primary bg-primary/10 border-primary/25 dark:text-primary-tint',
				secondary: 'text-muted-foreground bg-muted-foreground/10 border-muted-foreground/25',
				destructive: 'text-destructive bg-destructive/10 border-destructive/25',
				outline: 'text-muted-foreground bg-muted-foreground/10 border-muted-foreground/25',
				primary: 'text-primary bg-primary/10 border-primary/25 dark:text-primary-tint',
				red: 'text-destructive bg-destructive/10 border-destructive/25',
				orange: 'text-orange bg-orange/10 border-orange/25',
				amber: 'text-warning bg-warning/10 border-warning/25',
				lime: 'text-lime bg-lime/10 border-lime/25',
				green: 'text-mint bg-mint/10 border-mint/25',
				emerald: 'text-mint bg-mint/10 border-mint/25',
				teal: 'text-teal bg-teal/10 border-teal/25',
				cyan: 'text-cyan bg-cyan/10 border-cyan/25',
				sky: 'text-sky bg-sky/10 border-sky/25',
				blue: 'text-info bg-info/10 border-info/25',
				indigo: 'text-indigo bg-indigo/10 border-indigo/25',
				violet: 'text-violet bg-violet/10 border-violet/25',
				purple: 'text-purple bg-purple/10 border-purple/25',
				fuchsia: 'text-fuchsia bg-fuchsia/10 border-fuchsia/25',
				pink: 'text-pink bg-pink/10 border-pink/25',
				rose: 'text-rose bg-rose/10 border-rose/25',
				gray: 'text-muted-foreground bg-muted-foreground/10 border-muted-foreground/25'
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
		beam = false,
		children,
		...restProps
	}: WithElementRef<HTMLAnchorAttributes> & {
		variant?: BadgeVariant;
		size?: BadgeSize;
		minWidth?: BadgeMinWidth;
		hoverEffect?: BadgeHoverEffect;
		/** Monospace text for code-like values. */
		mono?: boolean;
		beam?: boolean;
	} = $props();
	void cn;
</script>

<svelte:element
	this={href ? 'a' : 'span'}
	bind:this={ref}
	data-slot="badge"
	{href}
	class={cn(badgeVariants({ variant, size, minWidth, hoverEffect, mono }), beam && 'relative overflow-visible', className)}
	{...restProps}
>
	{#if beam}
		<span aria-hidden="true" class="pointer-events-none absolute -inset-px animate-border-beam rounded-inherit border-beam"
		></span>
	{/if}
	{@render children?.()}
</svelte:element>
