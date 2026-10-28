<script lang="ts">
	import { onMount } from 'svelte';

	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Drawer from '#lib/components/ui/drawer/index.js';
	import * as Popover from '#lib/components/ui/popover/index.js';
	import { IsMobile } from '#lib/hooks/is-mobile.svelte.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';

	import ActivityCenterInspector from './activity-center-inspector.svelte';
	import ActivityCenterTray from './activity-center-tray.svelte';
	import { activityCenterLayout } from './activity-layout';

	type Anchor = HTMLElement | { getBoundingClientRect: () => DOMRect };

	const TRIGGER_SELECTOR = '[data-activity-center-trigger]';
	const TITLE_ID = 'activity-center-title';
	// Used when no trigger is on screen (e.g. opened from a toast with the mobile nav closed).
	const FALLBACK_ANCHOR: Anchor = { getBoundingClientRect: () => DOMRect.fromRect({ x: 12, y: 12, width: 0, height: 0 }) };

	const isMobile = new IsMobile();

	// "Open full view" from the tray shows the inspector until the center closes.
	let expanded = $state(false);
	let anchor = $state<Anchor>(FALLBACK_ANCHOR);

	const layout = $derived.by(() => {
		if (isMobile.current) return 'drawer';
		if (expanded || activityCenterLayout.current === 'inspector') return 'inspector';
		return 'tray';
	});

	onMount(() => {
		void activityStore.start();
		return () => activityStore.stop();
	});

	$effect(() => {
		if (!activityStore.open) {
			expanded = false;
			return;
		}
		anchor = visibleTriggerInternal() ?? FALLBACK_ANCHOR;
	});

	function visibleTriggerInternal(): HTMLElement | null {
		for (const element of document.querySelectorAll<HTMLElement>(TRIGGER_SELECTOR)) {
			if (element.getClientRects().length > 0) return element;
		}
		return null;
	}

	function setOpenInternal(open: boolean) {
		activityStore.setOpen(open);
	}

	// The trigger toggles the tray itself; letting the outside-click close it first would reopen it.
	function handleTrayInteractOutsideInternal(event: PointerEvent) {
		if (event.target instanceof Element && event.target.closest(TRIGGER_SELECTOR)) {
			event.preventDefault();
		}
	}
</script>

{#if layout === 'drawer'}
	<Drawer.Root open={activityStore.open} onOpenChange={setOpenInternal}>
		<Drawer.Content class="flex max-h-(--max-height-screen-85) flex-col overflow-hidden">
			<ActivityCenterTray dialogTitle />
		</Drawer.Content>
	</Drawer.Root>
{:else if layout === 'tray'}
	<Popover.Root open={activityStore.open} onOpenChange={setOpenInternal}>
		<Popover.Content
			variant="panel"
			role="dialog"
			aria-labelledby={TITLE_ID}
			customAnchor={anchor}
			side="right"
			align="start"
			sideOffset={12}
			collisionPadding={8}
			onInteractOutside={handleTrayInteractOutsideInternal}
			class="flex max-h-(--max-height-activity-tray) w-(--width-activity-tray) flex-col"
		>
			<ActivityCenterTray titleId={TITLE_ID} onExpand={() => (expanded = true)} />
		</Popover.Content>
	</Popover.Root>
{:else}
	<Dialog.Root open={activityStore.open} onOpenChange={setOpenInternal}>
		<Dialog.Content variant="side-panel" showCloseButton={false} class="w-(--width-activity-inspector)">
			<ActivityCenterInspector onClose={() => setOpenInternal(false)} />
		</Dialog.Content>
	</Dialog.Root>
{/if}
