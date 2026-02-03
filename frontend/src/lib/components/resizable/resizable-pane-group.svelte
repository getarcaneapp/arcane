<script lang="ts">
	import type { Snippet } from 'svelte';
	import { onDestroy } from 'svelte';
	import { ResizableGroup, setResizableGroupContext } from './resizable.svelte';

	interface Props {
		children: Snippet;
		orientation?: 'horizontal' | 'vertical';
		persistKey?: string;
		persistStorage?: 'local' | 'session';
		onLayoutChange?: () => void;
		class?: string;
	}

	let {
		children,
		orientation = 'horizontal',
		persistKey,
		persistStorage = 'session',
		onLayoutChange,
		class: className = ''
	}: Props = $props();

	const groupState = new ResizableGroup({
		orientation,
		persistKey,
		persistStorage,
		onLayoutChange
	});

	setResizableGroupContext(groupState);

	let containerRef: HTMLDivElement | null = $state(null);

	$effect(() => {
		groupState.containerRef = containerRef;
		if (containerRef) {
			groupState.setupResizeObserver(containerRef);
		}
	});

	onDestroy(() => {
		groupState.cleanupResizeObserver();
	});
</script>

<div
	bind:this={containerRef}
	data-resizable-group="true"
	class="flex min-h-0 min-w-0 {orientation === 'horizontal' ? 'flex-row' : 'flex-col'} {className}"
>
	{@render children()}
</div>
