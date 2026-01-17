<script lang="ts">
	import type { Snippet } from 'svelte';
	import { onDestroy, onMount } from 'svelte';
	import { PersistedState } from 'runed';
	import { DoubleArrowLeftIcon, DoubleArrowRightIcon } from '$lib/icons';

	interface Props {
		first: Snippet;
		second: Snippet;
		size?: number | null;
		minSize?: number;
		minSecondSize?: number;
		defaultRatio?: number;
		allowCollapse?: boolean;
		collapseThreshold?: number;
		handleSize?: number;
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
		minSecondSize = 240,
		defaultRatio = 0.5,
		allowCollapse = true,
		collapseThreshold = 28,
		handleSize = 8,
		class: className = '',
		firstClass = '',
		secondClass = '',
		handleClass = '',
		ariaLabel = 'Resize panels',
		persistKey,
		persistStorage = 'session',
		onResizeEnd = () => {}
	}: Props = $props();

	let containerRef = $state<HTMLDivElement | null>(null);
	let isResizing = $state(false);
	let collapsedSide = $state<'first' | 'second' | null>(null);
	let lastSize = minSize;
	let persistedState = $state<PersistedState<number | null> | null>(null);
	let persistedKey = $state<string | null>(null);

	let resizeStartX = 0;
	let resizeStartWidth = 0;

	const clampSize = (value: number, minValue: number, maxValue: number) => Math.min(Math.max(value, minValue), maxValue);

	const getAvailableWidth = () => {
		if (!containerRef) return 0;
		return Math.max(0, containerRef.getBoundingClientRect().width - handleSize);
	};

	const getMaxNormalSize = () => Math.max(0, getAvailableWidth() - minSecondSize);

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
			collapsedSide = 'first';
			size = 0;
			if (persist) commitSize();
			return;
		}
		if (allowCollapse && nextSize >= available - collapseThreshold) {
			collapsedSide = 'second';
			size = available;
			if (persist) commitSize();
			return;
		}

		collapsedSide = null;
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
		window.removeEventListener('pointermove', handleMove);
		window.removeEventListener('pointerup', stopResize);
		document.body.style.cursor = '';
		document.body.style.userSelect = '';
		commitSize();
		onResizeEnd();
	};

	const handleWindowResize = () => {
		if (isResizing) return;
		if (size === null) {
			ensureInitialSize();
			if (size === null) return;
		}
		applySize(size);
	};

	function startResize(event: PointerEvent) {
		if (!containerRef) return;
		ensureInitialSize();
		resizeStartX = event.clientX;
		resizeStartWidth = size ?? 0;
		isResizing = true;
		window.addEventListener('pointermove', handleMove);
		window.addEventListener('pointerup', stopResize);
		document.body.style.cursor = 'col-resize';
		document.body.style.userSelect = 'none';
		event.preventDefault();
	}

	function restoreCollapsed() {
		const maxNormal = getMaxNormalSize();
		const safeMin = Math.min(minSize, maxNormal);
		const restored = clampSize(lastSize || safeMin, safeMin, maxNormal);
		collapsedSide = null;
		size = restored;
		lastSize = restored;
		commitSize();
		onResizeEnd();
	}

	$effect(() => {
		if (!persistKey) {
			persistedState = null;
			persistedKey = null;
			return;
		}
		if (persistedKey === persistKey && persistedState) return;
		persistedState = new PersistedState<number | null>(persistKey, size ?? null, {
			storage: persistStorage,
			syncTabs: false
		});
		persistedKey = persistKey;
		const stored = persistedState.current;
		if (stored !== null && stored !== undefined) {
			size = stored;
		}
	});

	$effect(() => {
		if (!containerRef || size === null || isResizing) return;
		applySize(size, false);
	});

	onMount(() => {
		ensureInitialSize();
		window.addEventListener('resize', handleWindowResize);
		return () => window.removeEventListener('resize', handleWindowResize);
	});

	onDestroy(() => {
		window.removeEventListener('pointermove', handleMove);
		window.removeEventListener('pointerup', stopResize);
	});
</script>

<div bind:this={containerRef} class={`flex min-h-0 min-w-0 ${className}`}>
	<div
		class={`min-h-0 min-w-0 flex-none overflow-hidden ${firstClass}`}
		style={`width: ${size ?? 0}px;`}
		aria-hidden={collapsedSide === 'first'}
	>
		{#if collapsedSide !== 'first'}
			{@render first()}
		{/if}
	</div>

	<div
		role="separator"
		aria-orientation="vertical"
		aria-label={ariaLabel}
		class={`group relative z-20 flex shrink-0 cursor-col-resize items-stretch justify-center overflow-visible ${handleClass}`}
		style={`width: ${handleSize}px;`}
		onpointerdown={startResize}
	>
		<div class="bg-border group-hover:bg-primary/50 my-2 w-0.5 rounded-full transition-colors"></div>
		{#if collapsedSide}
			<button
				class="bg-background border-border text-muted-foreground hover:text-foreground focus-visible:ring-ring absolute inset-0 z-10 m-auto flex size-6 items-center justify-center rounded-full border shadow-sm focus-visible:ring-2 focus-visible:outline-none"
				onclick={(event) => {
					event.stopPropagation();
					restoreCollapsed();
				}}
				onpointerdown={(event) => event.stopPropagation()}
				aria-label={collapsedSide === 'first' ? 'Show left panel' : 'Show right panel'}
				title={collapsedSide === 'first' ? 'Show left panel' : 'Show right panel'}
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

	<div
		class={`min-h-0 min-w-0 flex-1 overflow-hidden ${secondClass}`}
		style={collapsedSide === 'second' ? 'flex: 0 0 0px; width: 0px;' : ''}
		aria-hidden={collapsedSide === 'second'}
	>
		{#if collapsedSide !== 'second'}
			{@render second()}
		{/if}
	</div>
</div>
