import { PersistedState } from 'runed';
import { createContext } from 'svelte';

import { IsTablet } from '#lib/hooks/is-tablet.svelte.js';

import { SIDEBAR_KEYBOARD_SHORTCUT } from './constants.js';

type Getter<T> = () => T;

export type SidebarStateProps = {
	/**
	 * A getter function that returns the current open state of the sidebar.
	 * We use a getter function here to support `bind:open` on the `Sidebar.Provider`
	 * component.
	 */
	open: Getter<boolean | undefined>;

	/**
	 * A function that sets the open state of the sidebar. To support `bind:open`, we need
	 * a source of truth for changing the open state to ensure it will be synced throughout
	 * the sub-components and any `bind:` references.
	 */
	setOpen: (open: boolean) => void;
};

class SidebarState {
	readonly props: SidebarStateProps;
	open = $derived.by(() => {
		if (this.#isTablet.current) return false;
		return this.props.open() ?? this.#isPinnedState.current;
	});
	setOpen: SidebarStateProps['setOpen'];
	#isTablet: IsTablet;
	#isPinnedState = new PersistedState('sidebar-pinned', true);
	state = $derived.by(() => (this.open ? 'expanded' : 'collapsed'));

	constructor(props: SidebarStateProps) {
		this.setOpen = props.setOpen;
		this.#isTablet = new IsTablet();
		this.props = props;
	}

	// Convenience getter for checking if the screen is tablet size
	get isTablet() {
		return this.#isTablet.current;
	}

	// Getter for pinning preference
	get isPinned() {
		return this.props.open() ?? this.#isPinnedState.current;
	}

	// Event handler to apply to the `<svelte:window>`
	handleShortcutKeydown = (e: KeyboardEvent) => {
		if (e.key === SIDEBAR_KEYBOARD_SHORTCUT && (e.metaKey || e.ctrlKey)) {
			e.preventDefault();
			// Don't allow keyboard toggle in tablet mode
			if (!this.#isTablet.current) {
				this.toggle();
			}
		}
	};

	toggle = () => {
		if (!this.#isTablet.current) {
			// On desktop, toggle the pinning preference
			this.#isPinnedState.current = !this.isPinned;
			return this.setOpen(this.#isPinnedState.current);
		}
	};
}

const [useSidebar, setSidebarContext] = createContext<SidebarState>();
export { useSidebar };

/**
 * Instantiates a new `SidebarState` instance and sets it in the context.
 *
 * @param props The constructor props for the `SidebarState` class.
 * @returns  The `SidebarState` instance.
 */
export function setSidebar(props: SidebarStateProps): SidebarState {
	return setSidebarContext(new SidebarState(props));
}
