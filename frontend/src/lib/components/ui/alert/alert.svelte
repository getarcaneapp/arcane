<script lang="ts" module>
	import { type VariantProps, tv } from 'tailwind-variants';

	// Without a title the icon centers on the description's first 20px line: 1rem of padding plus 2px.
	export const alertVariants = tv({
		base: 'group/alert [&>svg]:text-foreground relative w-full rounded-xl border bg-card p-4 [&_[data-slot=alert-description]]:text-muted-foreground [&>svg]:absolute [&>svg]:left-4 [&>svg]:top-4 [&:has(>svg+[data-slot=alert-description])>svg]:top-[1.125rem] [&>svg~*]:pl-7',
		variants: {
			variant: {
				default: 'text-foreground',
				warning: 'border-warning/30 bg-warning/8 text-foreground [&>svg]:text-warning',
				destructive: 'border-destructive/30 bg-destructive/8 text-foreground [&>svg]:text-destructive',
				info: 'border-info/30 bg-info/8 text-foreground [&>svg]:text-info',
				'warning-subtle': 'border-warning/20 bg-warning/5 [&>svg]:text-warning',
				'destructive-subtle': 'border-destructive/20 bg-destructive/5 [&>svg]:text-destructive',
				'primary-subtle': 'border-primary/20 bg-primary/5 [&>svg]:text-primary dark:border-primary/30 dark:bg-primary/10'
			},
			size: {
				default: '',
				sm: 'py-2.5 [&>svg]:top-1/2 [&>svg]:-translate-y-1/2 [&:has(>svg+[data-slot=alert-description])>svg]:top-1/2'
			}
		},
		defaultVariants: {
			variant: 'default',
			size: 'default'
		}
	});

	export type AlertVariant = VariantProps<typeof alertVariants>['variant'];
	export type AlertSize = VariantProps<typeof alertVariants>['size'];
</script>

<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import type { IconType } from '#lib/icons/index.js';
	import type { WithElementRef } from '#lib/utils.js';
	import { cn } from '#lib/utils.js';

	import Description from './alert-description.svelte';
	import Title from './alert-title.svelte';

	let {
		ref = $bindable(null),
		class: className,
		variant = 'default',
		size = 'default',
		children,
		icon: Icon,
		heading,
		description,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> & {
		variant?: AlertVariant;
		size?: AlertSize;
		icon?: IconType;
		heading?: string;
		description?: string;
	} = $props();
</script>

<div bind:this={ref} class={cn(alertVariants({ variant, size }), className)} data-size={size} {...restProps} role="alert">
	{#if Icon}<Icon class="size-4" />{/if}
	{#if heading}<Title>{heading}</Title>{/if}
	{#if description}<Description>{description}</Description>{/if}
	{@render children?.()}
</div>
