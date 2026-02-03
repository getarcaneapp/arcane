<script lang="ts">
	import { cn } from '$lib/utils';
	import { getResizableGroupContext } from './resizable.svelte';
	import { DoubleArrowLeftIcon, DoubleArrowRightIcon, DoubleArrowUpIcon, DoubleArrowDownIcon } from '$lib/icons';

	interface Props {
		index: number;
		collapsible?: 'before' | 'after';
		size?: number;
		class?: string;
	}

	let { index, collapsible, size = 8, class: className }: Props = $props();

	const ctx = getResizableGroupContext();

	function handlePointerDown(event: PointerEvent) {
		if (!canResize) return;
		ctx.startResize(index, event);
	}

	const isHorizontal = $derived(ctx.orientation === 'horizontal');

	// Get the pane IDs for before/after this handle
	const beforePaneId = $derived(ctx.getPaneIdAtIndex(index));
	const afterPaneId = $derived(ctx.getPaneIdAtIndex(index + 1));

	// Check if either adjacent pane is collapsed
	const beforeCollapsed = $derived(beforePaneId ? ctx.isCollapsed(beforePaneId) : false);
	const afterCollapsed = $derived(afterPaneId ? ctx.isCollapsed(afterPaneId) : false);
	const anyCollapsed = $derived(beforeCollapsed || afterCollapsed);

	// Check if this handle is at an edge with a collapsed pane (can't resize)
	const isFirstHandle = $derived(index === 0);
	const isLastHandle = $derived(index === ctx.panes.length - 2);
	const canResize = $derived(
		!(isFirstHandle && beforeCollapsed) && !(isLastHandle && afterCollapsed)
	);

	// Determine which pane can be collapsed based on the collapsible prop
	const collapsiblePaneId = $derived(collapsible === 'before' ? beforePaneId : collapsible === 'after' ? afterPaneId : null);

	// Check if the collapsible pane is actually marked as collapsible
	const canCollapse = $derived.by(() => {
		if (!collapsiblePaneId) return false;
		const pane = ctx.panes.find((p) => p.id === collapsiblePaneId);
		return pane?.collapsible ?? false;
	});

	function handleCollapseClick(event: MouseEvent) {
		event.stopPropagation();
		if (!collapsiblePaneId) return;
		ctx.toggle(collapsiblePaneId);
	}

	function handleRestoreClick(event: MouseEvent) {
		event.stopPropagation();
		if (beforeCollapsed && beforePaneId) {
			ctx.expand(beforePaneId);
		} else if (afterCollapsed && afterPaneId) {
			ctx.expand(afterPaneId);
		}
	}
</script>

<div
	role="separator"
	aria-orientation={isHorizontal ? 'vertical' : 'horizontal'}
	class={cn(
		'group relative flex shrink-0 items-center justify-center overflow-visible',
		isHorizontal ? 'mx-2 h-full' : 'my-2 w-full',
		canResize && (isHorizontal ? 'cursor-col-resize' : 'cursor-row-resize'),
		className
	)}
	style={isHorizontal ? `width: ${size}px;` : `height: ${size}px;`}
	onpointerdown={handlePointerDown}
>
	<div
		class={cn(
			'bg-border group-hover:bg-primary/50 rounded-full transition-colors',
			isHorizontal ? 'h-full w-0.5' : 'h-0.5 w-full'
		)}
	></div>

	{#if anyCollapsed}
		<button
			class={cn(
				'bg-background border-border text-muted-foreground hover:text-foreground focus-visible:ring-ring',
				'absolute top-1/2 left-1/2 flex size-6 -translate-x-1/2 -translate-y-1/2 items-center justify-center',
				'rounded-full border shadow-sm focus-visible:ring-2 focus-visible:outline-none'
			)}
			onclick={handleRestoreClick}
			onpointerdown={(e) => e.stopPropagation()}
			aria-label={beforeCollapsed ? 'Show left panel' : 'Show right panel'}
			title={beforeCollapsed ? 'Show left panel' : 'Show right panel'}
			type="button"
		>
			{#if isHorizontal}
				{#if beforeCollapsed}
					<DoubleArrowRightIcon class="size-4" />
				{:else}
					<DoubleArrowLeftIcon class="size-4" />
				{/if}
			{:else if beforeCollapsed}
				<DoubleArrowDownIcon class="size-4" />
			{:else}
				<DoubleArrowUpIcon class="size-4" />
			{/if}
		</button>
	{:else if collapsible && canCollapse}
		<button
			class={cn(
				'bg-background border-border text-muted-foreground hover:text-foreground focus-visible:ring-ring',
				'absolute top-1/2 left-1/2 flex size-6 -translate-x-1/2 -translate-y-1/2 items-center justify-center',
				'rounded-full border opacity-0 shadow-sm transition-opacity',
				'group-hover:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:outline-none'
			)}
			onclick={handleCollapseClick}
			onpointerdown={(e) => e.stopPropagation()}
			aria-label={collapsible === 'before' ? 'Collapse left panel' : 'Collapse right panel'}
			title={collapsible === 'before' ? 'Collapse left panel' : 'Collapse right panel'}
			type="button"
		>
			{#if isHorizontal}
				{#if collapsible === 'before'}
					<DoubleArrowLeftIcon class="size-4" />
				{:else}
					<DoubleArrowRightIcon class="size-4" />
				{/if}
			{:else if collapsible === 'before'}
				<DoubleArrowUpIcon class="size-4" />
			{:else}
				<DoubleArrowDownIcon class="size-4" />
			{/if}
		</button>
	{/if}
</div>
