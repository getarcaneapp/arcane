<script lang="ts" module>
	import { cn, type WithElementRef } from '#lib/utils.js';
	import type { WithChildren } from 'bits-ui';
	import type { HTMLAnchorAttributes, HTMLButtonAttributes } from 'svelte/elements';
	import { type VariantProps, tv } from 'tailwind-variants';

	export const buttonVariants = tv({
		base: "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-xl border text-sm font-medium outline-none transition-[background-color,border-color,color,box-shadow,transform,filter] duration-200 ease-out focus-visible:ring-2 focus-visible:ring-ring/70 focus-visible:ring-offset-2 focus-visible:ring-offset-background active:scale-[0.985] disabled:pointer-events-none disabled:opacity-55 disabled:saturate-[0.82] disabled:shadow-none aria-disabled:pointer-events-none aria-disabled:opacity-55 aria-disabled:saturate-[0.82] aria-disabled:shadow-none aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 [&_svg:not([class*='size-'])]:size-4 [&_svg]:pointer-events-none [&_svg]:shrink-0",
		variants: {
			variant: {
				default:
					'border-primary/70 bg-primary text-primary-foreground shadow-[inset_0_1px_0_rgba(255,255,255,0.16),0_10px_24px_-14px_rgba(15,23,42,0.35)] hover:border-primary/80 hover:bg-primary/92',
				destructive:
					'border-destructive/70 bg-destructive text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.14),0_10px_24px_-14px_rgba(15,23,42,0.3)] hover:border-destructive/80 hover:bg-destructive/92 focus-visible:ring-destructive/30 dark:bg-destructive/80 dark:focus-visible:ring-destructive/40',
				outline:
					'border-border/80 bg-background/90 text-foreground shadow-[inset_0_1px_0_rgba(255,255,255,0.34),0_1px_2px_rgba(15,23,42,0.05)] backdrop-blur-sm hover:border-border hover:bg-accent/60 hover:text-accent-foreground hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.24),0_8px_20px_-12px_rgba(15,23,42,0.22)] dark:bg-card/70 dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.06),0_1px_2px_rgba(0,0,0,0.16)] dark:hover:bg-accent/40',
				secondary:
					'border-border/70 bg-secondary text-secondary-foreground shadow-[inset_0_1px_0_rgba(255,255,255,0.22),0_1px_2px_rgba(15,23,42,0.05)] hover:border-border hover:bg-secondary/85',
				ghost:
					'border-transparent bg-transparent text-foreground shadow-none hover:bg-accent/50 hover:text-accent-foreground dark:hover:bg-accent/30',
				link: 'border-transparent bg-transparent text-primary shadow-none underline-offset-4 hover:bg-primary/5 hover:underline'
			},
			size: {
				default: 'h-9 px-4 py-2 has-[svg]:px-3',
				sm: 'h-8 gap-1.5 rounded-lg px-3 has-[svg]:px-2.5',
				lg: 'h-10 rounded-xl px-6 has-[svg]:px-4',
				icon: 'size-9 p-0'
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
