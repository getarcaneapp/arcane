import type { VariantProps } from 'tailwind-variants';

import { tv } from '#lib/utils.js';

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
