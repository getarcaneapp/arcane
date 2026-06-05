import Root from './field-set.svelte';
import Content from './field-set-content.svelte';
import Footer from './field-set-footer.svelte';
import Title from './field-set-title.svelte';
import type { FieldSetRootProps, FieldSetTitleProps, FieldSetContentProps, FieldSetFooterProps } from './types';
export { fieldSetVariants, type Variant } from './variants';

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
