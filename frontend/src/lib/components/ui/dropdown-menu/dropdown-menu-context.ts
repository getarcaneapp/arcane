import { createContext } from 'svelte';

export type DropdownMenuContext = {
	close: () => void;
};

export const [getDropdownMenuContext, setDropdownMenuContext, hasDropdownMenuContext] = createContext<DropdownMenuContext>();
