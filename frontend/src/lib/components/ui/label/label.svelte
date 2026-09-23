<script lang="ts" module>
	import { type VariantProps, tv } from 'tailwind-variants';

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
				normal: 'font-normal'
			}
		},
		defaultVariants: {
			variant: 'default',
			size: 'default',
			weight: 'medium'
		}
	});

	export type LabelVariant = VariantProps<typeof labelVariants>['variant'];
	export type LabelSize = VariantProps<typeof labelVariants>['size'];
	export type LabelWeight = VariantProps<typeof labelVariants>['weight'];
</script>

<script lang="ts">
	import { Label as LabelPrimitive } from 'bits-ui';
	import { cn } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		variant = 'default',
		size = 'default',
		weight = 'medium',
		...restProps
	}: LabelPrimitive.RootProps & { variant?: LabelVariant; size?: LabelSize; weight?: LabelWeight } = $props();
</script>

<LabelPrimitive.Root bind:ref class={cn(labelVariants({ variant, size, weight }), className)} {...restProps} />
