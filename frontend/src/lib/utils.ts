import { clsx, type ClassValue } from 'clsx';
import { extendTailwindMerge } from 'tailwind-merge';
import { createTV } from 'tailwind-variants';

const twMerge = extendTailwindMerge({
	extend: {
		theme: {
			text: ['2xs', '3xs', '4xs', 'xs-plus', 'form-control'],
			shadow: ['glow', 'glow-lg', 'float', 'button', 'button-hover'],
			tracking: ['ultrawide']
		}
	}
});

export const tv = createTV({ twMerge });

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}

export type WithoutChild<T> = T extends { child?: unknown } ? Omit<T, 'child'> : T;
export type WithoutChildren<T> = T extends { children?: unknown } ? Omit<T, 'children'> : T;
export type WithoutChildrenOrChild<T> = WithoutChildren<WithoutChild<T>>;
export type WithElementRef<T, U extends HTMLElement = HTMLElement> = T & { ref?: U | null };
