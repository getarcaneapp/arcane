<script lang="ts" module>
	import { cn, type WithElementRef } from '$lib/utils.js';
	import type { WithChildren } from 'bits-ui';
	import type { HTMLAnchorAttributes, HTMLButtonAttributes } from 'svelte/elements';
	import { type VariantProps, tv } from 'tailwind-variants';

	export const buttonVariants = tv({
		base: "focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-lg text-sm font-medium outline-none transition-all focus-visible:ring-[3px] disabled:pointer-events-none disabled:opacity-50 aria-disabled:pointer-events-none aria-disabled:opacity-50 [&_svg:not([class*='size-'])]:size-4 [&_svg]:pointer-events-none [&_svg]:shrink-0",
		variants: {
			variant: {
				default: 'bg-primary text-primary-foreground bubble-shadow hover:bg-primary/90',
				destructive:
					'bg-destructive bubble-shadow hover:bg-destructive/90 focus-visible:ring-destructive/20 dark:focus-visible:ring-destructive/40 dark:bg-destructive/60 text-white',
				outline: 'backdrop-blur-sm bg-card/60 border hover:backdrop-blur-sm hover:bg-card/90 hover:text-accent-foreground',
				secondary: 'bg-secondary text-secondary-foreground bubble-shadow hover:bg-secondary/80',
				ghost: 'hover:backdrop-blur-sm hover:bg-card/60 hover:text-accent-foreground',
				link: 'text-primary underline-offset-4 hover:underline'
			},
			size: {
				default: 'h-9 px-4 py-2 has-[svg]:px-3',
				sm: 'h-8 gap-1.5 rounded-lg px-3 has-[svg]:px-2.5',
				lg: 'h-10 rounded-lg px-6 has-[svg]:px-4',
				icon: 'size-9'
			},
			hoverEffect: {
				none: '',
				lift: 'hover-lift'
			}
		},
		defaultVariants: {
			variant: 'default',
			size: 'default',
			hoverEffect: 'none'
		}
	});

	export type ButtonVariant = VariantProps<typeof buttonVariants>['variant'];
	export type ButtonSize = VariantProps<typeof buttonVariants>['size'];
	export type ButtonHoverEffect = VariantProps<typeof buttonVariants>['hoverEffect'];

	export type ButtonPropsWithoutHTML = WithChildren<{
		ref?: HTMLElement | null;
		variant?: ButtonVariant;
		size?: ButtonSize;
		hoverEffect?: ButtonHoverEffect;
		loading?: boolean;
		onClickPromise?: (
			e: MouseEvent & {
				currentTarget: EventTarget & HTMLButtonElement;
			}
		) => Promise<void>;
	}>;

	export type ButtonProps = WithElementRef<HTMLButtonAttributes> &
		WithElementRef<HTMLAnchorAttributes> & {
			variant?: ButtonVariant;
			size?: ButtonSize;
			hoverEffect?: ButtonHoverEffect;
		};
</script>

<script lang="ts">
	let {
		class: className,
		variant = 'default',
		size = 'default',
		hoverEffect = 'none',
		ref = $bindable(null),
		href = undefined,
		type = 'button',
		disabled,
		children,
		...restProps
	}: ButtonProps = $props();
</script>

{#if href}
	<a
		bind:this={ref}
		data-slot="button"
		class={cn(buttonVariants({ variant, size, hoverEffect }), className)}
		href={disabled ? undefined : href}
		aria-disabled={disabled}
		role={disabled ? 'link' : undefined}
		tabindex={disabled ? -1 : undefined}
		{...restProps}
	>
		{@render children?.()}
	</a>
{:else}
	<button
		bind:this={ref}
		data-slot="button"
		class={cn(buttonVariants({ variant, size, hoverEffect }), className)}
		{type}
		{disabled}
		{...restProps}
	>
		{@render children?.()}
	</button>
{/if}
