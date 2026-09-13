<script lang="ts">
	import type { Snippet } from 'svelte';
	import { untrack } from 'svelte';
	import { PersistedState } from 'runed';
	import { DoubleArrowLeftIcon, DoubleArrowRightIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		first: Snippet;
		second: Snippet;
		size?: number | null;
		minSize?: number;
		maxSize?: number;
		minSecondSize?: number;
		defaultRatio?: number;
		allowCollapse?: boolean;
		collapseThreshold?: number;
		handleSize?: number;
		variant?: 'default' | 'flush';
		stackBelow?: number;
		class?: string;
		firstClass?: string;
		secondClass?: string;
		handleClass?: string;
		ariaLabel?: string;
		persistKey?: string;
		persistStorage?: 'local' | 'session';
		onResizeEnd?: () => void;
	}

	let {
		first,
		second,
		size = $bindable<number | null>(null),
		minSize = 240,
		maxSize,
		minSecondSize = 240,
		defaultRatio = 0.5,
		allowCollapse = true,
		collapseThreshold = 28,
		handleSize = 8,
		variant = 'default',
		stackBelow,
		class: className = '',
		firstClass = '',
		secondClass = '',
		handleClass = '',
		ariaLabel,
		persistKey,
		persistStorage = 'session',
		onResizeEnd = () => {}
	}: Props = $props();

	let containerRef: HTMLDivElement | null = null;
	let containerWidth = $state(0);
	let isResizing = false;
	let lastSize = 0;
	const initialPreferences = untrack(() => ({ key: persistKey, size, storage: persistStorage }));
	let persistedState: PersistedState<number | null> | null = null;
	if (initialPreferences.key) {
		persistedState = new PersistedState<number | null>(initialPreferences.key, initialPreferences.size, {
			storage: initialPreferences.storage,
			syncTabs: false
		});
	}
	const storedSize = persistedState?.current;
	if (storedSize !== null && storedSize !== undefined) size = storedSize;
	const isStacked = $derived(stackBelow !== undefined && containerWidth < stackBelow);
	let resizeStartX = 0;
	let resizeStartWidth = 0;

	const clampSize = (value: number, minValue: number, maxValue: number) => Math.min(Math.max(value, minValue), maxValue);

	const layoutHandleSize = $derived.by(() => {
		if (variant === 'flush') return 1;
		return handleSize;
	});

	const getAvailableWidth = () => Math.max(0, containerWidth - layoutHandleSize);

	const getMaxNormalSize = () => {
		const base = Math.max(0, getAvailableWidth() - minSecondSize);
		if (maxSize !== undefined) return Math.min(base, maxSize);
		return base;
	};

	const layout = $derived.by(() => {
		const available = getAvailableWidth();
		const requested = size ?? Math.round(available * defaultRatio);
		if (available <= 0) return { size: 0, collapsedSide: null };
		if (allowCollapse && requested <= collapseThreshold) return { size: 0, collapsedSide: 'first' };
		if (allowCollapse && maxSize === undefined && requested >= available - collapseThreshold) {
			return { size: available, collapsedSide: 'second' };
		}
		const maxNormal = getMaxNormalSize();
		return { size: clampSize(requested, Math.min(minSize, maxNormal), maxNormal), collapsedSide: null };
	});
	const collapsedSide = $derived(layout.collapsedSide);

	const ensureInitialSize = () => {
		if (!containerRef || size !== null) return;
		const available = getAvailableWidth();
		if (available <= 0) return;
		const maxNormal = getMaxNormalSize();
		const safeMin = Math.min(minSize, maxNormal);
		const initialSize = clampSize(Math.round(available * defaultRatio), safeMin, maxNormal);
		size = initialSize;
		lastSize = initialSize;
	};

	const commitSize = () => {
		if (!persistedState || size === null) return;
		persistedState.current = size;
	};

	const applySize = (nextSize: number, persist = false) => {
		const available = getAvailableWidth();
		if (available <= 0) return;
		const maxNormal = getMaxNormalSize();
		const safeMin = Math.min(minSize, maxNormal);

		if (allowCollapse && nextSize <= collapseThreshold) {
			size = 0;
			if (persist) commitSize();
			return;
		}
		if (allowCollapse && maxSize === undefined && nextSize >= available - collapseThreshold) {
			size = available;
			if (persist) commitSize();
			return;
		}

		const clamped = clampSize(nextSize, safeMin, maxNormal);
		size = clamped;
		lastSize = clamped;
		if (persist) commitSize();
	};

	const handleMove = (event: PointerEvent) => {
		if (!isResizing) return;
		applySize(resizeStartWidth + (event.clientX - resizeStartX));
	};

	const stopResize = () => {
		if (!isResizing) return;
		isResizing = false;
		document.body.style.cursor = '';
		document.body.style.userSelect = '';
		commitSize();
		onResizeEnd();
	};

	function measureContainer(node: HTMLDivElement) {
		containerRef = node;
		containerWidth = node.getBoundingClientRect().width;
		const observer = new ResizeObserver(() => {
			containerWidth = node.getBoundingClientRect().width;
			if (isResizing || isStacked) return;
			ensureInitialSize();
			if (size !== null) applySize(size);
		});
		observer.observe(node);
		return () => {
			observer.disconnect();
			containerRef = null;
			if (!isResizing) return;
			isResizing = false;
			document.body.style.cursor = '';
			document.body.style.userSelect = '';
		};
	}

	function startResize(event: PointerEvent) {
		if (!containerRef) return;
		ensureInitialSize();
		resizeStartX = event.clientX;
		resizeStartWidth = size ?? 0;
		isResizing = true;
		document.body.style.cursor = 'col-resize';
		document.body.style.userSelect = 'none';
		event.preventDefault();
	}

	function restoreCollapsed() {
		const maxNormal = getMaxNormalSize();
		const safeMin = Math.min(minSize, maxNormal);
		const restored = clampSize(lastSize || safeMin, safeMin, maxNormal);
		size = restored;
		lastSize = restored;
		commitSize();
		onResizeEnd();
	}
</script>

<svelte:window onpointermove={handleMove} onpointerup={stopResize} />

<div
	{@attach measureContainer}
	class={['flex min-h-0 min-w-0', isStacked && (variant === 'flush' ? 'flex-col' : 'flex-col gap-4'), className]}
>
	<div
		class={['min-h-0 min-w-0 overflow-hidden', isStacked ? 'flex-1' : 'flex-none', firstClass]}
		style={isStacked ? '' : `width: ${layout.size}px;`}
		aria-hidden={!isStacked && collapsedSide === 'first'}
	>
		{#if isStacked || collapsedSide !== 'first'}
			{@render first()}
		{/if}
	</div>

	{#if !isStacked}
		<div
			role="separator"
			aria-orientation="vertical"
			aria-label={ariaLabel ?? m.common_resize_panels()}
			class={[
				'group relative z-[var(--arcane-z-sticky)] flex shrink-0 cursor-col-resize items-stretch justify-center overflow-visible',
				handleClass
			]}
			style={`width: ${layoutHandleSize}px;`}
			onpointerdown={startResize}
		>
			{#if variant === 'flush'}
				<div class="w-px bg-border transition-colors group-hover:bg-primary/50"></div>
				<div class="absolute inset-y-0 -right-1 -left-1"></div>
			{:else}
				<div class="my-2 w-0.5 rounded-full bg-border transition-colors group-hover:bg-primary/50"></div>
			{/if}
			{#if collapsedSide}
				<button
					class="absolute inset-0 z-[var(--arcane-z-raised)] m-auto flex size-6 items-center justify-center rounded-full border border-border bg-background text-muted-foreground shadow-sm hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
					onclick={(event) => {
						event.stopPropagation();
						restoreCollapsed();
					}}
					onpointerdown={(event) => event.stopPropagation()}
					aria-label={collapsedSide === 'first' ? m.common_show_left_panel() : m.common_show_right_panel()}
					title={collapsedSide === 'first' ? m.common_show_left_panel() : m.common_show_right_panel()}
					type="button"
				>
					{#if collapsedSide === 'first'}
						<DoubleArrowRightIcon class="size-4" />
					{:else}
						<DoubleArrowLeftIcon class="size-4" />
					{/if}
				</button>
			{/if}
		</div>
	{/if}

	<div
		class={['min-h-0 min-w-0 flex-1 overflow-hidden', secondClass]}
		style={!isStacked && collapsedSide === 'second' ? 'flex: 0 0 0px; width: 0px;' : ''}
		aria-hidden={!isStacked && collapsedSide === 'second'}
	>
		{#if isStacked || collapsedSide !== 'second'}
			{@render second()}
		{/if}
	</div>
</div>
