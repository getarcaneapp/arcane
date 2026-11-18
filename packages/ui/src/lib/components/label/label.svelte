<script lang="ts" module>
	import type { VariantProps } from 'tailwind-variants';

	import { tv, cn } from '#lib/utils.js';

	export const labelVariants = tv({
		base: 'text-sm leading-none font-medium peer-disabled:cursor-not-allowed peer-disabled:opacity-70',
		variants: {
			variant: {
				default: '',
				muted: 'text-muted-foreground',
				destructive: 'text-destructive'
			},
			size: {
				default: '',
				xs: 'text-xs',
				base: 'text-base'
			},
			weight: {
				medium: '',
				normal: 'font-normal',
				semibold: 'font-semibold'
			},
			leading: {
				none: '',
				snug: 'leading-5'
			}
		},
		defaultVariants: {
			variant: 'default',
			size: 'default',
			weight: 'medium',
			leading: 'none'
		}
	});

	export type LabelVariant = VariantProps<typeof labelVariants>['variant'];
	export type LabelSize = VariantProps<typeof labelVariants>['size'];
	export type LabelWeight = VariantProps<typeof labelVariants>['weight'];
	export type LabelLeading = VariantProps<typeof labelVariants>['leading'];
</script>

<script lang="ts">
	import { Label as LabelPrimitive } from 'bits-ui';

	let {
		ref = $bindable(null),
		class: className,
		variant = 'default',
		size = 'default',
		weight = 'medium',
		leading = 'none',
		...restProps
	}: LabelPrimitive.RootProps & {
		variant?: LabelVariant;
		size?: LabelSize;
		weight?: LabelWeight;
		leading?: LabelLeading;
	} = $props();
</script>

<LabelPrimitive.Root bind:ref class={cn(labelVariants({ variant, size, weight, leading }), className)} {...restProps} />
