import type { WithChildren, WithoutChildren } from 'bits-ui';
import type { HTMLAttributes } from 'svelte/elements';

import type { Variant } from '.';

export type FieldSetRootPropsWithoutHTML = WithChildren<{
	ref?: HTMLDivElement | null;
	variant?: Variant;
}>;

export type FieldSetRootProps = FieldSetRootPropsWithoutHTML & WithoutChildren<HTMLAttributes<HTMLDivElement>>;

export type FieldSetContentPropsWithoutHTML = WithChildren<{
	ref?: HTMLDivElement | null;
}>;

export type FieldSetContentProps = FieldSetContentPropsWithoutHTML & WithoutChildren<HTMLAttributes<HTMLDivElement>>;

export type FieldSetTitlePropsWithoutHtml = WithChildren<{
	ref?: HTMLHeadingElement | null;
	level?: 1 | 2 | 3 | 4 | 5 | 6;
}>;

export type FieldSetTitleProps = FieldSetTitlePropsWithoutHtml & WithoutChildren<HTMLAttributes<HTMLHeadingElement>>;

export type FieldSetFooterPropsWithoutHTML = WithChildren<{
	ref?: HTMLDivElement | null;
}>;

export type FieldSetFooterProps = FieldSetFooterPropsWithoutHTML & WithoutChildren<HTMLAttributes<HTMLDivElement>>;
