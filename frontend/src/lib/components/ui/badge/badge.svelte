<script lang="ts" module>
	import { type VariantProps, tv } from 'tailwind-variants';

	export const badgeVariants = tv({
		base: 'focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden whitespace-nowrap rounded-lg border px-2.5 py-1 text-xs font-medium transition-all focus-visible:ring-[3px] [&>svg]:pointer-events-none [&>svg]:size-3',
		variants: {
			variant: {
				default: 'bubble-pill bg-primary text-primary-foreground [a&]:hover:bg-primary/90 border-transparent',
				secondary: 'bubble-pill bg-secondary text-secondary-foreground [a&]:hover:bg-secondary/90 border-transparent',
				destructive:
					'bubble-pill bg-destructive [a&]:hover:bg-destructive/90 focus-visible:ring-destructive/20 dark:focus-visible:ring-destructive/40 dark:bg-destructive/70 border-transparent text-white',
				outline:
					'backdrop-blur-sm bg-card/60 text-foreground [a&]:hover:backdrop-blur-sm [a&]:hover:bg-card/90 [a&]:hover:text-accent-foreground'
			},
			hoverEffect: {
				none: '',
				lift: '[a&]:hover-lift'
			}
		},
		defaultVariants: {
			variant: 'default',
			hoverEffect: 'lift'
		}
	});

	export type BadgeVariant = VariantProps<typeof badgeVariants>['variant'];
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
		hoverEffect = 'lift',
		children,
		...restProps
	}: WithElementRef<HTMLAnchorAttributes> & {
		variant?: BadgeVariant;
		hoverEffect?: BadgeHoverEffect;
	} = $props();
</script>

<svelte:element
	this={href ? 'a' : 'span'}
	bind:this={ref}
	data-slot="badge"
	{href}
	class={cn(badgeVariants({ variant, hoverEffect }), className)}
	{...restProps}
>
	{@render children?.()}
</svelte:element>
