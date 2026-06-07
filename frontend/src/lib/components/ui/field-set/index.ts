import Root from './field-set.svelte';
import Content from './field-set-content.svelte';
import Footer from './field-set-footer.svelte';
import Title from './field-set-title.svelte';
import type { FieldSetRootProps, FieldSetTitleProps, FieldSetContentProps, FieldSetFooterProps } from './types';
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

export {
	Root,
	Content,
	Footer,
	Title,
	type FieldSetRootProps as RootProps,
	type FieldSetTitleProps as TitleProps,
	type FieldSetContentProps as ContentProps,
	type FieldSetFooterProps as FooterProps
};
