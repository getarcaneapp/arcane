import { createContext } from 'svelte';

export interface ArcaneTooltipContext {
	isTouch: boolean;
	interactive: boolean;
	open: boolean;
	setOpen: (value: boolean) => void;
}

export const [getArcaneTooltipContext, setArcaneTooltipContext] = createContext<ArcaneTooltipContext>();
