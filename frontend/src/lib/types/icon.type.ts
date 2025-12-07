import type { Component } from 'svelte';

export type PhosphorIconWeight = 'thin' | 'light' | 'regular' | 'bold' | 'fill' | 'duotone';

export interface PhosphorIconProps {
	color?: string;
	size?: number | string;
	weight?: PhosphorIconWeight;
	mirrored?: boolean;
	class?: string;
	'aria-hidden'?: boolean | 'true' | 'false';
	[key: `data-${string}`]: any;
}

export type PhosphorIcon = Component<PhosphorIconProps, {}, any>;
