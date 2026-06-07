import {
	Virtualizer,
	elementScroll,
	observeElementOffset,
	observeElementRect,
	type VirtualItem,
	type VirtualizerOptions
} from '@tanstack/virtual-core';

// The three platform adapters (`observeElementRect` / `observeElementOffset` / `scrollToFn`) are
// supplied for the caller, so they only provide the data-driven options (count, getScrollElement,
// estimateSize, …).
type SuppliedKeys = 'observeElementRect' | 'observeElementOffset' | 'scrollToFn';
type CreateVirtualizerOptions<TScroll extends Element, TItem extends Element> = Omit<
	VirtualizerOptions<TScroll, TItem>,
	SuppliedKeys
> &
	Partial<Pick<VirtualizerOptions<TScroll, TItem>, SuppliedKeys>>;

/**
 * A Svelte 5 runes wrapper around `@tanstack/virtual-core`.
 *
 * We wrap the framework-agnostic core directly (rather than `@tanstack/svelte-virtual`, whose v3
 * adapter still uses Svelte 4 stores and mis-tracks the scroll-element binding under runes) — the
 * same approach the repo took for `@tanstack/table-core`. `onChange` pushes the recomputed window
 * into `$state`, and `$effect.pre` re-applies options whenever the reactive inputs (row count,
 * scroll element, …) change.
 *
 * Pass `options` as a thunk so its reactive reads are tracked.
 */
export function createVirtualizer<TScroll extends Element, TItem extends Element>(
	options: () => CreateVirtualizerOptions<TScroll, TItem>
) {
	let virtualItems = $state.raw<VirtualItem[]>([]);
	let totalSize = $state(0);

	function resolveOptions(): VirtualizerOptions<TScroll, TItem> {
		const supplied = options();
		return {
			observeElementRect,
			observeElementOffset,
			scrollToFn: elementScroll,
			...supplied,
			onChange: (instance, sync) => {
				virtualItems = instance.getVirtualItems();
				totalSize = instance.getTotalSize();
				supplied.onChange?.(instance, sync);
			}
		};
	}

	const instance = new Virtualizer<TScroll, TItem>(resolveOptions());
	virtualItems = instance.getVirtualItems();
	totalSize = instance.getTotalSize();

	// Re-apply options on reactive input changes, then recompute the visible window.
	$effect.pre(() => {
		instance.setOptions(resolveOptions());
		instance._willUpdate();
		virtualItems = instance.getVirtualItems();
		totalSize = instance.getTotalSize();
	});

	// Attach scroll/resize observers after the DOM (and the bound scroll element) exists.
	$effect(() => instance._didMount());

	return {
		get virtualItems() {
			return virtualItems;
		},
		get totalSize() {
			return totalSize;
		},
		/** `use:` action target — measures real row heights for accurate offsets. */
		measureElement: (node: TItem) => instance.measureElement(node)
	};
}
