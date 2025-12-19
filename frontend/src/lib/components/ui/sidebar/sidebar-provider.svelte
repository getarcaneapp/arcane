<script lang="ts">
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { cn, type WithElementRef } from '$lib/utils.js';
	import type { HTMLAttributes } from 'svelte/elements';
	import { SIDEBAR_COOKIE_MAX_AGE, SIDEBAR_COOKIE_NAME, SIDEBAR_WIDTH, SIDEBAR_WIDTH_ICON } from './constants.js';
	import { setSidebar } from './context.svelte.js';
	import { IsTablet } from '$lib/hooks/is-tablet.svelte.js';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';
	import { PersistedState } from 'runed';
	import settingsStore from '$lib/stores/config-store';

	const persistedPinned = new PersistedState('sidebar-pinned', true);

	let {
		ref = $bindable(null),
		open = $bindable(persistedPinned.current),
		onOpenChange = () => {},
		class: className,
		style,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> & {
		open?: boolean;
		onOpenChange?: (open: boolean) => void;
	} = $props();

	// Initialize breakpoint detection utilities
	const isTablet = new IsTablet();
	const isMobile = new IsMobile();
	let initialAdjustmentDone = $state(false);

	// Set initial state based on screen size (only runs once)
	$effect(() => {
		if (!initialAdjustmentDone) {
			// On tablet screens (but not mobile), force collapse the sidebar
			if (isTablet.current && !isMobile.current) {
				open = false;
			}
			// On desktop screens (1024px and above), auto-expand the sidebar
			else if (!isTablet.current && !isMobile.current && !open) {
				open = true;
			}
			initialAdjustmentDone = true;
		}
	});

	const sidebar = setSidebar({
		open: () => open,
		setOpen: (value: boolean) => {
			// Don't allow expanding sidebar on tablet screens
			if (isTablet.current && !isMobile.current && value === true) {
				return;
			}

			open = value;
			onOpenChange(value);

			// This sets the cookie to keep the sidebar state.
			document.cookie = `${SIDEBAR_COOKIE_NAME}=${open}; path=/; max-age=${SIDEBAR_COOKIE_MAX_AGE}`;
		}
	});

	// Sync sidebar hover expansion with backend settings
	$effect(() => {
		const settings = $settingsStore;
		if (settings && settings.sidebarHoverExpansion !== undefined) {
			sidebar.setHoverExpansion(settings.sidebarHoverExpansion);
		}
	});
</script>

<svelte:window onkeydown={sidebar.handleShortcutKeydown} />

<Tooltip.Provider delayDuration={0}>
	<div
		data-slot="sidebar-wrapper"
		style="--sidebar-width: {SIDEBAR_WIDTH}; --sidebar-width-icon: {SIDEBAR_WIDTH_ICON}; {style}"
		class={cn('group/sidebar-wrapper has-data-[variant=inset]:bg-sidebar flex h-dvh w-full', className)}
		bind:this={ref}
		{...restProps}
	>
		{@render children?.()}
	</div>
</Tooltip.Provider>
