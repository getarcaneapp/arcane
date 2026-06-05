import { tv, type VariantProps } from 'tailwind-variants';

export const fieldSetVariants = tv({
	base: 'border-border flex h-fit w-full flex-col rounded-lg border',
	variants: {
		variant: {
			default: 'border-border bg-card',
			destructive: 'border-destructive'
		}
	}
});

export type Variant = VariantProps<typeof fieldSetVariants>['variant'];
