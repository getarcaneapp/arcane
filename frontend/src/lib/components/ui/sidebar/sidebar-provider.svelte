<script lang="ts">
	import * as Tooltip from '#lib/components/ui/tooltip/index.js';
	import { cn, type WithElementRef } from '#lib/utils.js';
	import type { HTMLAttributes } from 'svelte/elements';
	import { SIDEBAR_COOKIE_MAX_AGE, SIDEBAR_COOKIE_NAME, SIDEBAR_WIDTH, SIDEBAR_WIDTH_ICON } from './constants.js';
	import { setSidebar } from './context.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';

	let {
		ref = $bindable(null),
		open = $bindable<boolean | undefined>(undefined),
		onOpenChange = () => {},
		class: className,
		style,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> & {
		open?: boolean;
		onOpenChange?: (open: boolean) => void;
	} = $props();

	const sidebar = setSidebar({
		open: () => open,
		setOpen: (value: boolean) => {
			if (open !== undefined) open = value;
			onOpenChange(value);

			// This sets the cookie to keep the sidebar state.
			document.cookie = `${SIDEBAR_COOKIE_NAME}=${value}; path=/; max-age=${SIDEBAR_COOKIE_MAX_AGE}`;
		}
	});
</script>

<svelte:window
	onkeydown={(event) => {
		if (userStore.current?.preferences?.keyboardShortcutsEnabled !== false) {
			sidebar.handleShortcutKeydown(event);
		}
	}}
/>

<Tooltip.Provider delayDuration={0}>
	<div
		data-slot="sidebar-wrapper"
		style="--sidebar-width: {SIDEBAR_WIDTH}; --sidebar-width-icon: {SIDEBAR_WIDTH_ICON}; --sidebar-gap-width: {sidebar.open
			? SIDEBAR_WIDTH
			: SIDEBAR_WIDTH_ICON}; {style}"
		class={cn('group/sidebar-wrapper flex h-dvh w-full has-data-[variant=inset]:bg-sidebar', className)}
		bind:this={ref}
		{...restProps}
	>
		{@render children?.()}
	</div>
</Tooltip.Provider>
