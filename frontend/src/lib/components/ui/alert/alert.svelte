<script lang="ts" module>
	import { type VariantProps, tv } from 'tailwind-variants';

	export const alertVariants = tv({
		base: 'group/alert [&>svg]:text-foreground relative w-full rounded-xl border p-4 backdrop-blur-sm bg-card/80 [&>svg]:absolute [&>svg]:left-4 [&>svg]:top-4 [&>svg~*]:pl-7',
		variants: {
			variant: {
				default: 'text-foreground',
				warning: 'border-warning/50 text-warning [&>svg]:text-warning',
				destructive: 'border-destructive/50 text-destructive dark:border-destructive [&>svg]:text-destructive',
				info: 'border-info/30 bg-info/10 text-info [&>svg]:text-info',
				'warning-subtle': 'border-warning/30 bg-warning/10',
				'destructive-subtle': 'border-destructive/30 bg-destructive/10',
				'primary-subtle': 'border-primary/20 bg-primary/5 dark:border-primary/30 dark:bg-primary/10'
			},
			size: {
				default: '',
				sm: 'py-2 [&>svg]:top-2'
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
	import type { WithElementRef } from '#lib/utils.js';
	import { cn } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		variant = 'default',
		size = 'default',
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> & {
		variant?: AlertVariant;
		size?: AlertSize;
	} = $props();
</script>

<div bind:this={ref} class={cn(alertVariants({ variant, size }), className)} data-size={size} {...restProps} role="alert">
	{@render children?.()}
</div>
