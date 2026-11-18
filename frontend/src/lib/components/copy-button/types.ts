/*
	Installed from @ieedan/shadcn-svelte-extras
*/

import type { ButtonPropsWithoutHTML } from '@arcane/ui/button';
import type { WithChildren, WithoutChildren } from 'bits-ui';
import type { Snippet } from 'svelte';
import type { HTMLAttributes } from 'svelte/elements';

import type { UseClipboard } from '#lib/hooks/use-clipboard.svelte.js';

export type CopyButtonPropsWithoutHTML = WithChildren<
	Pick<ButtonPropsWithoutHTML, 'variant'> & {
		size?: Exclude<ButtonPropsWithoutHTML['size'], 'inline'>;
		ref?: HTMLButtonElement | null;
		text: string;
		icon?: Snippet<[]>;
		animationDuration?: number;
		onCopy?: (status: UseClipboard['status']) => void;
	}
>;

export type CopyButtonProps = CopyButtonPropsWithoutHTML & WithoutChildren<HTMLAttributes<HTMLButtonElement>>;
