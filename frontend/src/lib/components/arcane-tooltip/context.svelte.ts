import { getContext, setContext } from 'svelte';

const CONTEXT_KEY = 'arcane-tooltip-context';

export interface ArcaneTooltipContext {
	isTouch: boolean;
	interactive: boolean;
	open: boolean;
	setOpen: (value: boolean) => void;
}

export function setArcaneTooltipContext(context: ArcaneTooltipContext) {
	setContext(CONTEXT_KEY, context);
}

export function getArcaneTooltipContext(): ArcaneTooltipContext {
	return getContext(CONTEXT_KEY);
}
