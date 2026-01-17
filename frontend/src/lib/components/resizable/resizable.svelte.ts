import { getContext, setContext } from 'svelte';
import { PersistedState } from 'runed';

export interface PaneConfig {
	id: string;
	minSize: number;
	defaultSize?: number;
	collapsible?: boolean;
	collapsedSize?: number;
	flex?: boolean;
}

export interface ResizableGroupState {
	orientation: 'horizontal' | 'vertical';
	panes: PaneConfig[];
	sizes: Map<string, number>;
	collapsedPanes: Set<string>;
	containerRef: HTMLDivElement | null;
	isResizing: boolean;
	registerPane: (config: PaneConfig) => void;
	unregisterPane: (id: string) => void;
	getSize: (id: string) => number;
	setSize: (id: string, size: number) => void;
	isCollapsed: (id: string) => boolean;
	isFlex: (id: string) => boolean;
	collapse: (id: string) => void;
	expand: (id: string) => void;
	toggle: (id: string) => void;
	startResize: (handleIndex: number, event: PointerEvent) => void;
	getPaneIdAtIndex: (index: number) => string | null;
}

interface PersistedLayout {
	sizes: Record<string, number>;
	collapsed: string[];
}

const RESIZABLE_GROUP_KEY = Symbol('resizable-group');

export function setResizableGroupContext(state: ResizableGroupState) {
	setContext(RESIZABLE_GROUP_KEY, state);
}

export function getResizableGroupContext(): ResizableGroupState {
	const context = getContext<ResizableGroupState>(RESIZABLE_GROUP_KEY);
	if (!context) {
		throw new Error('Resizable components must be used within a ResizablePaneGroup');
	}
	return context;
}

export class ResizableGroup implements ResizableGroupState {
	orientation: 'horizontal' | 'vertical';
	panes = $state<PaneConfig[]>([]);
	sizes = $state<Map<string, number>>(new Map());
	collapsedPanes = $state<Set<string>>(new Set());
	containerRef = $state<HTMLDivElement | null>(null);
	isResizing = $state(false);

	private persistKey?: string;
	private persistStorage: 'local' | 'session';
	private onLayoutChange?: () => void;
	private persistedState: PersistedState<PersistedLayout> | null = null;

	private resizingHandleIndex = -1;
	private resizeStartPos = 0;
	private resizeObserver: ResizeObserver | null = null;
	private lastContainerSize = 0;

	constructor(options: {
		orientation?: 'horizontal' | 'vertical';
		persistKey?: string;
		persistStorage?: 'local' | 'session';
		onLayoutChange?: () => void;
	}) {
		this.orientation = options.orientation ?? 'horizontal';
		this.persistKey = options.persistKey;
		this.persistStorage = options.persistStorage ?? 'session';
		this.onLayoutChange = options.onLayoutChange;

		// Initialize persistence
		if (this.persistKey) {
			this.persistedState = new PersistedState<PersistedLayout>(
				this.persistKey,
				{ sizes: {}, collapsed: [] },
				{
					storage: this.persistStorage,
					syncTabs: false
				}
			);

			const stored = this.persistedState.current;
			if (stored) {
				// Restore sizes
				if (stored.sizes) {
					for (const [id, size] of Object.entries(stored.sizes)) {
						this.sizes.set(id, size);
					}
					this.sizes = new Map(this.sizes);
				}
				// Restore collapsed state
				if (stored.collapsed) {
					this.collapsedPanes = new Set(stored.collapsed);
				}
			}
		}
	}

	private commitLayout() {
		if (!this.persistedState) return;
		
		// Only save sizes for non-flex panes (flex panes recalculate on load)
		const sizesToSave: Record<string, number> = {};
		for (const pane of this.panes) {
			if (!pane.flex) {
				const size = this.sizes.get(pane.id);
				if (size !== undefined) {
					sizesToSave[pane.id] = size;
				}
			}
		}
		
		const layout: PersistedLayout = {
			sizes: sizesToSave,
			collapsed: Array.from(this.collapsedPanes)
		};
		this.persistedState.current = layout;
		this.onLayoutChange?.();
	}

	/**
	 * Set up ResizeObserver to watch container size changes
	 */
	setupResizeObserver(container: HTMLDivElement) {
		this.cleanupResizeObserver();
		
		this.resizeObserver = new ResizeObserver((entries) => {
			for (const entry of entries) {
				const newSize = this.orientation === 'horizontal' 
					? entry.contentRect.width 
					: entry.contentRect.height;
				
				// Only react if size actually changed and we're not currently resizing
				if (newSize !== this.lastContainerSize && !this.isResizing) {
					this.lastContainerSize = newSize;
					this.enforceContainerConstraints();
				}
			}
		});
		
		this.resizeObserver.observe(container);
		
		// Listen for nested groups requesting space
		container.addEventListener('resizable-request-space', this.handleNestedSpaceRequest);
		
		// Store initial size
		const rect = container.getBoundingClientRect();
		this.lastContainerSize = this.orientation === 'horizontal' ? rect.width : rect.height;
	}

	cleanupResizeObserver() {
		if (this.resizeObserver) {
			this.resizeObserver.disconnect();
			this.resizeObserver = null;
		}
		
		// Also remove the event listener
		if (this.containerRef) {
			this.containerRef.removeEventListener('resizable-request-space', this.handleNestedSpaceRequest);
		}
	}

	/**
	 * Measure total handle space from DOM
	 */
	private measureHandleSpace(): number {
		if (!this.containerRef) return 0;
		
		const handles = this.containerRef.querySelectorAll(':scope > [role="separator"]');
		let totalHandleSpace = 0;
		
		handles.forEach((handle) => {
			const rect = handle.getBoundingClientRect();
			const style = window.getComputedStyle(handle);
			
			if (this.orientation === 'horizontal') {
				const marginLeft = parseFloat(style.marginLeft) || 0;
				const marginRight = parseFloat(style.marginRight) || 0;
				totalHandleSpace += rect.width + marginLeft + marginRight;
			} else {
				const marginTop = parseFloat(style.marginTop) || 0;
				const marginBottom = parseFloat(style.marginBottom) || 0;
				totalHandleSpace += rect.height + marginTop + marginBottom;
			}
		});
		
		return totalHandleSpace;
	}

	/**
	 * Enforce container constraints by auto-collapsing panes if needed
	 */
	private enforceContainerConstraints() {
		if (!this.containerRef || this.panes.length === 0) return;
		
		const containerRect = this.containerRef.getBoundingClientRect();
		const containerSize = this.orientation === 'horizontal' ? containerRect.width : containerRect.height;
		
		if (containerSize <= 0) return; // Container not rendered yet
		
		// Measure actual handle space from DOM
		const handleSpace = this.measureHandleSpace();
		
		// Calculate minimum required space using effective min sizes
		let minRequired = handleSpace;
		const collapsiblePanes: Array<{ pane: PaneConfig; effectiveMin: number; index: number }> = [];
		
		for (let i = 0; i < this.panes.length; i++) {
			const pane = this.panes[i];
			
			if (this.isCollapsed(pane.id)) {
				minRequired += pane.collapsedSize ?? 0;
			} else {
				// Get effective min size (accounts for nested pane groups)
				const paneEl = this.containerRef.querySelector(`[data-pane-id="${pane.id}"]`);
				const effectiveMin = paneEl 
					? this.getEffectiveMinSize(paneEl, pane) 
					: pane.minSize;
				
				minRequired += effectiveMin;
				
				if (pane.collapsible) {
					collapsiblePanes.push({ pane, effectiveMin, index: i });
				}
			}
		}
		
		// If we're overflowing, collapse panes until we fit
		if (minRequired > containerSize && collapsiblePanes.length > 0) {
			// Sort collapsible panes by index (furthest from center first, then edges)
			// This prefers collapsing edge panes first
			const sortedCollapsible = [...collapsiblePanes].sort((a, b) => {
				// Prefer higher indices (rightmost/bottommost) first
				return b.index - a.index;
			});
			
			let changed = false;
			
			for (const { pane, effectiveMin } of sortedCollapsible) {
				if (minRequired <= containerSize) break;
				
				// Collapse this pane
				const savedSpace = effectiveMin - (pane.collapsedSize ?? 0);
				this.collapsedPanes.add(pane.id);
				minRequired -= savedSpace;
				changed = true;
			}
			
			if (changed) {
				this.collapsedPanes = new Set(this.collapsedPanes);
				this.commitLayout();
			}
		}
	}

	registerPane(config: PaneConfig) {
		this.panes = [...this.panes, config];
		// Only set default size if not already persisted and not a flex pane
		if (!this.sizes.has(config.id) && !config.flex) {
			this.sizes.set(config.id, config.defaultSize ?? config.minSize);
			this.sizes = new Map(this.sizes);
		}
	}

	unregisterPane(id: string) {
		this.panes = this.panes.filter((p) => p.id !== id);
		this.sizes.delete(id);
		this.collapsedPanes.delete(id);
	}

	getSize(id: string): number {
		return this.sizes.get(id) ?? 0;
	}

	setSize(id: string, size: number) {
		const pane = this.panes.find((p) => p.id === id);
		if (!pane) return;
		const clamped = Math.max(pane.minSize, size);
		this.sizes.set(id, clamped);
		this.sizes = new Map(this.sizes);
	}

	isCollapsed(id: string): boolean {
		return this.collapsedPanes.has(id);
	}

	isFlex(id: string): boolean {
		const pane = this.panes.find((p) => p.id === id);
		return pane?.flex ?? false;
	}

	collapse(id: string) {
		const pane = this.panes.find((p) => p.id === id);
		if (!pane?.collapsible) return;
		this.collapsedPanes.add(id);
		this.collapsedPanes = new Set(this.collapsedPanes);
		this.commitLayout();
	}

	expand(id: string) {
		const paneToExpand = this.panes.find((p) => p.id === id);
		if (!paneToExpand) return;
		
		// Check available space
		if (this.containerRef) {
			const containerRect = this.containerRef.getBoundingClientRect();
			const containerSize = this.orientation === 'horizontal' ? containerRect.width : containerRect.height;
			
			// Measure actual handle space
			const handleSpace = this.measureHandleSpace();
			
			// Calculate total min required after expanding this pane (using effective min sizes)
			let minRequired = handleSpace;
			const paneIndex = this.panes.indexOf(paneToExpand);
			
			for (let i = 0; i < this.panes.length; i++) {
				const pane = this.panes[i];
				const paneEl = this.containerRef.querySelector(`[data-pane-id="${pane.id}"]`);
				
				if (pane.id === id) {
					// This pane will be expanded - use its effective min
					const effectiveMin = paneEl 
						? this.getEffectiveMinSize(paneEl, pane) 
						: pane.minSize;
					minRequired += effectiveMin;
				} else if (this.isCollapsed(pane.id)) {
					minRequired += pane.collapsedSize ?? 0;
				} else {
					// Use effective min for expanded panes
					const effectiveMin = paneEl 
						? this.getEffectiveMinSize(paneEl, pane) 
						: pane.minSize;
					minRequired += effectiveMin;
				}
			}
			
			// If expanding would overflow, collapse other panes first
			if (minRequired > containerSize) {
				// Find collapsible panes that aren't this one, sorted by distance from this pane (furthest first)
				const collapsiblePanes = this.panes
					.filter(p => p.id !== id && p.collapsible && !this.isCollapsed(p.id))
					.map(p => {
						const paneEl = this.containerRef?.querySelector(`[data-pane-id="${p.id}"]`);
						const effectiveMin = paneEl 
							? this.getEffectiveMinSize(paneEl, p) 
							: p.minSize;
						return { pane: p, index: this.panes.indexOf(p), effectiveMin };
					})
					.sort((a, b) => {
						// Sort by distance from paneToExpand, furthest first
						const distA = Math.abs(a.index - paneIndex);
						const distB = Math.abs(b.index - paneIndex);
						return distB - distA;
					});
				
				let spaceToFree = minRequired - containerSize;
				
				for (const { pane, effectiveMin } of collapsiblePanes) {
					if (spaceToFree <= 0) break;
					
					const savedSpace = effectiveMin - (pane.collapsedSize ?? 0);
					this.collapsedPanes.add(pane.id);
					spaceToFree -= savedSpace;
				}
			}
		}
		
		// Now expand the target pane
		this.collapsedPanes.delete(id);
		this.collapsedPanes = new Set(this.collapsedPanes);
		this.commitLayout();
	}

	toggle(id: string) {
		if (this.isCollapsed(id)) {
			this.expand(id);
		} else {
			this.collapse(id);
		}
	}

	getPaneIdAtIndex(index: number): string | null {
		if (index < 0 || index >= this.panes.length) return null;
		return this.panes[index].id;
	}

	/**
	 * Calculate the effective minimum size of a pane, accounting for nested pane groups
	 */
	private getEffectiveMinSize(paneEl: Element, pane: PaneConfig): number {
		// Find all nested panes within this pane
		const allNestedPanes = paneEl.querySelectorAll('[data-pane-id]');
		if (allNestedPanes.length === 0) {
			return pane.minSize;
		}

		// Find the immediate nested pane group by looking for panes whose parent 
		// contains multiple [data-pane-id] children (indicating it's a pane group)
		let nestedGroupContainer: Element | null = null;
		for (const nestedPane of allNestedPanes) {
			const parent = nestedPane.parentElement;
			if (parent && parent !== paneEl) {
				const siblingPanes = parent.querySelectorAll(':scope > [data-pane-id]');
				if (siblingPanes.length > 1) {
					nestedGroupContainer = parent;
					break;
				}
			}
		}

		if (!nestedGroupContainer) {
			return pane.minSize;
		}

		// Sum up min-width/min-height of all direct child panes in this group
		let totalMinSize = 0;
		const childPanes = nestedGroupContainer.querySelectorAll(':scope > [data-pane-id]');
		
		childPanes.forEach((childPane) => {
			const style = window.getComputedStyle(childPane);
			const minSize = this.orientation === 'horizontal' 
				? parseFloat(style.minWidth) || 0
				: parseFloat(style.minHeight) || 0;
			totalMinSize += minSize;
		});

		// Count handles (elements with role="separator" that are direct children)
		const handles = nestedGroupContainer.querySelectorAll(':scope > [role="separator"]');
		
		// Measure actual handle sizes including margins
		handles.forEach((handle) => {
			const rect = handle.getBoundingClientRect();
			const style = window.getComputedStyle(handle);
			
			if (this.orientation === 'horizontal') {
				const marginLeft = parseFloat(style.marginLeft) || 0;
				const marginRight = parseFloat(style.marginRight) || 0;
				totalMinSize += rect.width + marginLeft + marginRight;
			} else {
				const marginTop = parseFloat(style.marginTop) || 0;
				const marginBottom = parseFloat(style.marginBottom) || 0;
				totalMinSize += rect.height + marginTop + marginBottom;
			}
		});

		// Return the larger of the pane's own minSize or the nested content's minSize
		return Math.max(pane.minSize, totalMinSize);
	}

	startResize(handleIndex: number, event: PointerEvent) {
		if (!this.containerRef) return;
		this.isResizing = true;
		this.resizingHandleIndex = handleIndex;
		
		const startPos = this.orientation === 'horizontal' ? event.clientX : event.clientY;
		this.resizeStartPos = startPos;
		this.lastMovePos = startPos;

		// Sync tracked sizes to actual rendered sizes
		for (const p of this.panes) {
			const paneEl = this.containerRef?.querySelector(`[data-pane-id="${p.id}"]`);
			if (paneEl) {
				const rect = paneEl.getBoundingClientRect();
				const size = this.orientation === 'horizontal' ? rect.width : rect.height;
				this.sizes.set(p.id, size);
			}
		}
		this.sizes = new Map(this.sizes);

		window.addEventListener('pointermove', this.handleMove);
		window.addEventListener('pointerup', this.stopResize);
		document.body.style.cursor = this.orientation === 'horizontal' ? 'col-resize' : 'row-resize';
		document.body.style.userSelect = 'none';
		event.preventDefault();
	}

	private lastMovePos = 0;

	/**
	 * Request space from parent pane groups when we're constrained.
	 * Uses a synchronous custom event to get space from ancestors.
	 * Returns the amount of space actually obtained.
	 */
	private requestParentResize(amount: number, direction: 'left' | 'right'): number {
		if (!this.containerRef || amount <= 0) return 0;

		// Create a custom event with a result field that parent can populate
		const eventDetail = {
			amount,
			direction,
			orientation: this.orientation,
			spaceProvided: 0 // Parent will update this
		};
		
		const event = new CustomEvent('resizable-request-space', {
			bubbles: true,
			cancelable: true,
			detail: eventDetail
		});

		this.containerRef.dispatchEvent(event);
		
		// Return how much space the parent(s) provided
		return eventDetail.spaceProvided;
	}

	/**
	 * Handle space requests from nested pane groups.
	 * This runs synchronously, updating detail.spaceProvided with how much we gave.
	 */
	handleNestedSpaceRequest = (event: Event) => {
		const customEvent = event as CustomEvent<{
			amount: number;
			direction: 'left' | 'right';
			orientation: 'horizontal' | 'vertical';
			spaceProvided: number;
		}>;

		// Only handle if orientation matches
		if (customEvent.detail.orientation !== this.orientation) return;

		// Find the pane that contains the nested group that dispatched this event
		const target = event.target as Element;
		let parentPane: PaneConfig | null = null;
		let parentPaneIndex = -1;

		for (let i = 0; i < this.panes.length; i++) {
			const pane = this.panes[i];
			const paneEl = this.containerRef?.querySelector(`[data-pane-id="${pane.id}"]`);
			if (paneEl && paneEl.contains(target)) {
				parentPane = pane;
				parentPaneIndex = i;
				break;
			}
		}

		if (!parentPane || parentPaneIndex < 0 || !this.containerRef) return;

		// Try to get space from sibling panes
		const { amount, direction } = customEvent.detail;
		let remainingAmount = amount;

		// Get current sizes
		const currentSizes: number[] = [];
		const effectiveMins: number[] = [];

		for (const p of this.panes) {
			const paneEl = this.containerRef.querySelector(`[data-pane-id="${p.id}"]`);
			if (paneEl) {
				const rect = paneEl.getBoundingClientRect();
				currentSizes.push(this.orientation === 'horizontal' ? rect.width : rect.height);
				effectiveMins.push(this.getEffectiveMinSize(paneEl, p));
			} else {
				currentSizes.push(this.getSize(p.id));
				effectiveMins.push(p.minSize);
			}
		}

		const newSizes = [...currentSizes];

		if (direction === 'left') {
			// Nested group wants to expand right, needs space from left siblings
			for (let i = parentPaneIndex - 1; i >= 0 && remainingAmount > 0; i--) {
				const available = newSizes[i] - effectiveMins[i];
				if (available > 0) {
					const take = Math.min(available, remainingAmount);
					newSizes[i] -= take;
					remainingAmount -= take;
				}
			}
		} else {
			// Nested group wants to expand left, needs space from right siblings
			for (let i = parentPaneIndex + 1; i < this.panes.length && remainingAmount > 0; i++) {
				const available = newSizes[i] - effectiveMins[i];
				if (available > 0) {
					const take = Math.min(available, remainingAmount);
					newSizes[i] -= take;
					remainingAmount -= take;
				}
			}
		}

		// Calculate how much space we freed locally
		let locallyFreed = amount - remainingAmount;
		
		// If we still need more, ask our parent
		if (remainingAmount > 0) {
			const fromAncestor = this.requestParentResize(remainingAmount, direction);
			locallyFreed += fromAncestor;
			remainingAmount -= fromAncestor;
		}

		// Give freed space to the parent pane
		if (locallyFreed > 0) {
			newSizes[parentPaneIndex] += locallyFreed;

			// Apply sizes
			for (let i = 0; i < this.panes.length; i++) {
				this.setSize(this.panes[i].id, newSizes[i]);
			}
		}
		
		// Report back how much space we provided (locally + from ancestors)
		customEvent.detail.spaceProvided += locallyFreed;
		
		// Always stop propagation - we handle cascading ourselves
		event.stopPropagation();
	};

	/**
	 * Helper to get current live sizes and effective mins from DOM
	 */
	private measurePaneSizes(): { sizes: number[]; mins: number[] } {
		const sizes: number[] = [];
		const mins: number[] = [];
		
		for (const p of this.panes) {
			const paneEl = this.containerRef?.querySelector(`[data-pane-id="${p.id}"]`);
			if (paneEl) {
				const rect = paneEl.getBoundingClientRect();
				sizes.push(this.orientation === 'horizontal' ? rect.width : rect.height);
				mins.push(this.getEffectiveMinSize(paneEl, p));
			} else {
				sizes.push(this.getSize(p.id));
				mins.push(p.minSize);
			}
		}
		
		return { sizes, mins };
	}

	private handleMove = (event: PointerEvent) => {
		if (this.resizingHandleIndex < 0 || !this.containerRef) return;

		const currentPos = this.orientation === 'horizontal' ? event.clientX : event.clientY;
		
		// Use incremental delta from last position, not from start
		// This allows smooth, continuous resizing that adapts to current state
		const delta = currentPos - this.lastMovePos;
		this.lastMovePos = currentPos;

		if (Math.abs(delta) < 1) return; // Ignore tiny movements

		const handleIndex = this.resizingHandleIndex;

		// Get current live sizes from DOM
		const { sizes: currentSizes, mins: effectiveMins } = this.measurePaneSizes();
		const newSizes = [...currentSizes];

		if (delta > 0) {
			// Expanding left side (panel at handleIndex): take space from right panels
			let remainingDelta = delta;

			// Take from right panels, closest first
			for (let i = handleIndex + 1; i < this.panes.length && remainingDelta > 0; i++) {
				const available = newSizes[i] - effectiveMins[i];
				if (available > 0) {
					const take = Math.min(available, remainingDelta);
					newSizes[i] -= take;
					remainingDelta -= take;
				}
			}

			// If we couldn't take enough locally, request from parent
			// Parent will shrink its siblings and grow our container
			// The space it provides becomes available for our expanding pane
			if (remainingDelta > 0) {
				const spaceFromParent = this.requestParentResize(remainingDelta, 'right');
				remainingDelta -= spaceFromParent;
			}

			// Give all obtained space (local + from parent) to the expanding pane
			const actualDelta = delta - remainingDelta;
			newSizes[handleIndex] += actualDelta;

		} else if (delta < 0) {
			// Expanding right side (panel at handleIndex + 1): take space from left panels
			let remainingDelta = Math.abs(delta);

			// Take from left panels, closest first
			for (let i = handleIndex; i >= 0 && remainingDelta > 0; i--) {
				const available = newSizes[i] - effectiveMins[i];
				if (available > 0) {
					const take = Math.min(available, remainingDelta);
					newSizes[i] -= take;
					remainingDelta -= take;
				}
			}

			// If we couldn't take enough locally, request from parent
			if (remainingDelta > 0) {
				const spaceFromParent = this.requestParentResize(remainingDelta, 'left');
				remainingDelta -= spaceFromParent;
			}

			// Give all obtained space (local + from parent) to the expanding pane
			const actualDelta = Math.abs(delta) - remainingDelta;
			if (handleIndex + 1 < this.panes.length) {
				newSizes[handleIndex + 1] += actualDelta;
			}
		}

		// Apply all new sizes
		for (let i = 0; i < this.panes.length; i++) {
			this.setSize(this.panes[i].id, newSizes[i]);
		}
	};

	private stopResize = () => {
		this.resizingHandleIndex = -1;
		this.isResizing = false;
		window.removeEventListener('pointermove', this.handleMove);
		window.removeEventListener('pointerup', this.stopResize);
		document.body.style.cursor = '';
		document.body.style.userSelect = '';
		this.commitLayout();
	};
}
