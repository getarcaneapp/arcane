<script lang="ts">
	import type { Snippet } from 'svelte';
	import { onMount, onDestroy } from 'svelte';
	import { getResizableGroupContext } from './resizable.svelte';

	interface Props {
		children: Snippet;
		id: string;
		minSize?: number;
		defaultSize?: number;
		collapsible?: boolean;
		collapsedSize?: number;
		flex?: boolean;
		class?: string;
	}

	let {
		children,
		id,
		minSize = 100,
		defaultSize,
		collapsible = false,
		collapsedSize = 0,
		flex = false,
		class: className = ''
	}: Props = $props();

	const ctx = getResizableGroupContext();

	onMount(() => {
		ctx.registerPane({
			id,
			minSize,
			defaultSize,
			collapsible,
			collapsedSize,
			flex
		});
	});

	onDestroy(() => {
		ctx.unregisterPane(id);
	});

	const size = $derived(ctx.getSize(id));
	const isCollapsed = $derived(ctx.isCollapsed(id));
	const isResizing = $derived(ctx.isResizing);

	// Check if any sibling pane is collapsed AND there's at least one flex pane to absorb the space
	const hasFlexPane = $derived.by(() => {
		for (const pane of ctx.panes) {
			if (pane.flex && !ctx.isCollapsed(pane.id)) {
				return true;
			}
		}
		return false;
	});

	const anySiblingCollapsed = $derived.by(() => {
		for (const pane of ctx.panes) {
			if (pane.id !== id && ctx.isCollapsed(pane.id)) {
				return true;
			}
		}
		return false;
	});

	// Determine if this pane should use flex sizing
	// Use flex when:
	// - This pane has flex prop AND not currently resizing, OR
	// - A sibling is collapsed AND there's no flex pane to absorb space AND not resizing
	//   (fallback: all panes flex to fill space when no designated flex pane exists)
	const shouldUseFlex = $derived(
		(flex || (anySiblingCollapsed && !hasFlexPane)) && !isResizing
	);

	const style = $derived.by(() => {
		if (isCollapsed) {
			return ctx.orientation === 'horizontal'
				? `width: ${collapsedSize}px; min-width: ${collapsedSize}px; max-width: ${collapsedSize}px; flex: 0 0 auto;`
				: `height: ${collapsedSize}px; min-height: ${collapsedSize}px; max-height: ${collapsedSize}px; flex: 0 0 auto;`;
		}

		if (shouldUseFlex) {
			// Flex sizing: fill remaining space, can grow and shrink
			return ctx.orientation === 'horizontal'
				? `min-width: ${minSize}px; flex: 1 1 0%;`
				: `min-height: ${minSize}px; flex: 1 1 0%;`;
		}

		// Fixed sizing: use tracked pixel size as basis, allow shrinking to min
		return ctx.orientation === 'horizontal'
			? `min-width: ${minSize}px; flex: 0 1 ${size}px;`
			: `min-height: ${minSize}px; flex: 0 1 ${size}px;`;
	});
</script>

<div
	class="relative min-h-0 min-w-0 overflow-hidden {className}"
	{style}
	aria-hidden={isCollapsed}
	data-pane-id={id}
	data-collapsed={isCollapsed}
	data-min-size={minSize}
	data-collapsible={collapsible ? '' : undefined}
	data-collapsed-size={collapsedSize}
>
	{#if !isCollapsed}
		{@render children()}
	{/if}
</div>
