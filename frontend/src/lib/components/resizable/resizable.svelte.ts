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

type GroupElement = HTMLDivElement & { __resizableGroup?: ResizableGroup };

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

	private persistedState: PersistedState<PersistedLayout> | null = null;
	private onLayoutChange?: () => void;
	private resizeObserver: ResizeObserver | null = null;
	private resizingHandleIndex = -1;
	private lastContainerSize = 0;
	private lastMovePos = 0;

	constructor(options: {
		orientation?: 'horizontal' | 'vertical';
		persistKey?: string;
		persistStorage?: 'local' | 'session';
		onLayoutChange?: () => void;
	}) {
		this.orientation = options.orientation ?? 'horizontal';
		this.onLayoutChange = options.onLayoutChange;

		if (options.persistKey) {
			this.persistedState = new PersistedState<PersistedLayout>(
				options.persistKey,
				{ sizes: {}, collapsed: [] },
				{ storage: options.persistStorage ?? 'session', syncTabs: false }
			);

			const stored = this.persistedState.current;
			if (stored?.sizes) {
				for (const [id, size] of Object.entries(stored.sizes)) {
					this.sizes.set(id, size);
				}
				this.sizes = new Map(this.sizes);
			}
			if (stored?.collapsed) {
				this.collapsedPanes = new Set(stored.collapsed);
			}
		}
	}

	private commitLayout() {
		if (!this.persistedState) return;

		const sizesToSave: Record<string, number> = {};
		for (const pane of this.panes) {
			if (!pane.flex) {
				const size = this.sizes.get(pane.id);
				if (size !== undefined) sizesToSave[pane.id] = size;
			}
		}

		this.persistedState.current = {
			sizes: sizesToSave,
			collapsed: Array.from(this.collapsedPanes)
		};
		this.onLayoutChange?.();
	}

	private updateCollapsed(mutate: (set: Set<string>) => void) {
		mutate(this.collapsedPanes);
		this.collapsedPanes = new Set(this.collapsedPanes);
		this.commitLayout();
	}

	setupResizeObserver(container: HTMLDivElement) {
		this.cleanupResizeObserver();
		(container as GroupElement).__resizableGroup = this;

		this.resizeObserver = new ResizeObserver((entries) => {
			for (const entry of entries) {
				const newSize =
					this.orientation === 'horizontal' ? entry.contentRect.width : entry.contentRect.height;
				if (newSize !== this.lastContainerSize && !this.isResizing) {
					this.lastContainerSize = newSize;
					this.enforceConstraints();
				}
			}
		});

		this.resizeObserver.observe(container);
		container.addEventListener('resizable-request-space', this.handleSpaceRequest);
		container.addEventListener('resizable-request-collapse', this.handleCollapseRequest);

		const rect = container.getBoundingClientRect();
		this.lastContainerSize = this.orientation === 'horizontal' ? rect.width : rect.height;
	}

	cleanupResizeObserver() {
		this.resizeObserver?.disconnect();
		this.resizeObserver = null;

		if (this.containerRef) {
			this.containerRef.removeEventListener('resizable-request-space', this.handleSpaceRequest);
			this.containerRef.removeEventListener('resizable-request-collapse', this.handleCollapseRequest);
			delete (this.containerRef as GroupElement).__resizableGroup;
		}
	}

	private getContainerSize(): number {
		if (!this.containerRef) return 0;
		const rect = this.containerRef.getBoundingClientRect();
		return this.orientation === 'horizontal' ? rect.width : rect.height;
	}

	private getPaneEl(id: string): Element | null {
		return this.containerRef?.querySelector(`[data-pane-id="${id}"]`) ?? null;
	}

	private getNestedGroup(paneEl: Element): ResizableGroup | null {
		const nested = paneEl.querySelector('[data-resizable-group="true"]') as GroupElement | null;
		const group = nested?.__resizableGroup;
		return group && group !== this ? group : null;
	}

	private measureHandles(container: Element = this.containerRef!): number {
		if (!container) return 0;
		let total = 0;
		container.querySelectorAll(':scope > [role="separator"]').forEach((handle) => {
			const rect = handle.getBoundingClientRect();
			const style = window.getComputedStyle(handle);
			if (this.orientation === 'horizontal') {
				total += rect.width + (parseFloat(style.marginLeft) || 0) + (parseFloat(style.marginRight) || 0);
			} else {
				total += rect.height + (parseFloat(style.marginTop) || 0) + (parseFloat(style.marginBottom) || 0);
			}
		});
		return total;
	}

	private getEffectiveMin(pane: PaneConfig): number {
		const paneEl = this.getPaneEl(pane.id);
		if (!paneEl) return pane.minSize;

		const nestedContainer = paneEl.querySelector('[data-resizable-group="true"]');
		if (!nestedContainer) return pane.minSize;

		let total = 0;
		nestedContainer.querySelectorAll(':scope > [data-pane-id]').forEach((child) => {
			if (child.getAttribute('data-collapsed') === 'true') {
				total += parseFloat(child.getAttribute('data-collapsed-size') || '0');
			} else {
				const minAttr = child.getAttribute('data-min-size');
				if (minAttr) {
					total += parseFloat(minAttr);
				} else {
					const style = window.getComputedStyle(child);
					total += parseFloat(this.orientation === 'horizontal' ? style.minWidth : style.minHeight) || 0;
				}
			}
		});

		total += this.measureHandles(nestedContainer);
		return Math.max(pane.minSize, total);
	}

	private calcMinRequired(): number {
		let total = this.measureHandles();
		for (const pane of this.panes) {
			total += this.isCollapsed(pane.id) ? (pane.collapsedSize ?? 0) : this.getEffectiveMin(pane);
		}
		return total;
	}

	private enforceConstraints() {
		if (!this.containerRef || this.panes.length === 0) return;

		const containerSize = this.getContainerSize();
		if (containerSize <= 0) return;

		let minRequired = this.calcMinRequired();
		if (minRequired <= containerSize) return;

		const collapsible = this.panes
			.filter((p) => p.collapsible && !this.isCollapsed(p.id))
			.map((p, _, arr) => ({ pane: p, index: arr.indexOf(p), min: this.getEffectiveMin(p) }))
			.sort((a, b) => b.index - a.index);

		let changed = false;
		for (const { pane, min } of collapsible) {
			if (minRequired <= containerSize) break;
			this.collapsedPanes.add(pane.id);
			minRequired -= min - (pane.collapsedSize ?? 0);
			changed = true;
		}

		if (changed) {
			this.collapsedPanes = new Set(this.collapsedPanes);
			this.commitLayout();
		}
	}

	registerPane(config: PaneConfig) {
		this.panes = [...this.panes, config];
		if (!this.sizes.has(config.id) && config.defaultSize !== undefined) {
			this.sizes.set(config.id, config.defaultSize);
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
		this.sizes.set(id, Math.max(pane.minSize, size));
		this.sizes = new Map(this.sizes);
	}

	isCollapsed(id: string): boolean {
		return this.collapsedPanes.has(id);
	}

	isFlex(id: string): boolean {
		return this.panes.find((p) => p.id === id)?.flex ?? false;
	}

	collapse(id: string) {
		const pane = this.panes.find((p) => p.id === id);
		if (pane?.collapsible) this.updateCollapsed((s) => s.add(id));
	}

	expand(id: string) {
		const pane = this.panes.find((p) => p.id === id);
		if (!pane) return;

		if (this.containerRef) {
			const containerSize = this.getContainerSize();
			const handleSpace = this.measureHandles();
			const paneIndex = this.panes.indexOf(pane);

			let minRequired = handleSpace;
			for (const p of this.panes) {
				if (p.id === id || !this.isCollapsed(p.id)) {
					minRequired += this.getEffectiveMin(p);
				} else {
					minRequired += p.collapsedSize ?? 0;
				}
			}

			if (minRequired > containerSize) {
				let spaceToFree = minRequired - containerSize;

				const collapsible = this.panes
					.filter((p) => p.id !== id && p.collapsible && !this.isCollapsed(p.id))
					.map((p) => ({ pane: p, index: this.panes.indexOf(p), min: this.getEffectiveMin(p) }))
					.sort((a, b) => Math.abs(b.index - paneIndex) - Math.abs(a.index - paneIndex));

				for (const { pane: p, index, min } of collapsible) {
					if (spaceToFree <= 0) break;

					const paneEl = this.getPaneEl(p.id);
					if (paneEl) {
						const nested = this.getNestedGroup(paneEl);
						if (nested) {
							const side = paneIndex < index ? 'right' : 'left';
							spaceToFree -= nested.collapsePanesToFree(spaceToFree, side);
							if (spaceToFree <= 0) continue;
						}
					}

					this.collapsedPanes.add(p.id);
					spaceToFree -= min - (p.collapsedSize ?? 0);
				}

				if (spaceToFree > 0) {
					spaceToFree -= this.requestParentCollapse(spaceToFree, paneIndex);
				}
			}
		}

		this.updateCollapsed((s) => s.delete(id));
	}

	toggle(id: string) {
		this.isCollapsed(id) ? this.expand(id) : this.collapse(id);
	}

	/** Collapse panes to free space. Returns amount freed. */
	collapsePanesToFree(amount: number, preferSide: 'left' | 'right'): number {
		if (!this.containerRef || amount <= 0) return 0;

		const collapsible = this.panes
			.filter((p) => p.collapsible && !this.isCollapsed(p.id))
			.map((p) => ({ pane: p, index: this.panes.indexOf(p), min: this.getEffectiveMin(p) }))
			.sort((a, b) => (preferSide === 'right' ? b.index - a.index : a.index - b.index));

		let freed = 0;
		for (const { pane, min } of collapsible) {
			if (freed >= amount) break;
			this.collapsedPanes.add(pane.id);
			freed += min - (pane.collapsedSize ?? 0);
		}

		if (freed > 0) {
			this.collapsedPanes = new Set(this.collapsedPanes);
			this.commitLayout();
		}
		return freed;
	}

	getPaneIdAtIndex(index: number): string | null {
		return this.panes[index]?.id ?? null;
	}

	startResize(handleIndex: number, event: PointerEvent) {
		if (!this.containerRef) return;
		this.isResizing = true;
		this.resizingHandleIndex = handleIndex;
		this.lastMovePos = this.orientation === 'horizontal' ? event.clientX : event.clientY;

		for (const p of this.panes) {
			const el = this.getPaneEl(p.id);
			if (el) {
				const rect = el.getBoundingClientRect();
				this.sizes.set(p.id, this.orientation === 'horizontal' ? rect.width : rect.height);
			}
		}
		this.sizes = new Map(this.sizes);

		window.addEventListener('pointermove', this.handleMove);
		window.addEventListener('pointerup', this.stopResize);
		document.body.style.cursor = this.orientation === 'horizontal' ? 'col-resize' : 'row-resize';
		document.body.style.userSelect = 'none';
		event.preventDefault();
	}

	private findParentPane(target: Element): { pane: PaneConfig; index: number } | null {
		for (let i = 0; i < this.panes.length; i++) {
			const el = this.getPaneEl(this.panes[i].id);
			if (el?.contains(target)) return { pane: this.panes[i], index: i };
		}
		return null;
	}

	private requestParentCollapse(amount: number, expandingIndex: number): number {
		if (!this.containerRef || amount <= 0) return 0;
		const detail = { amount, expandingPaneIndex: expandingIndex, orientation: this.orientation, spaceFreed: 0 };
		this.containerRef.dispatchEvent(new CustomEvent('resizable-request-collapse', { bubbles: true, detail }));
		return detail.spaceFreed;
	}

	private requestParentResize(amount: number, direction: 'left' | 'right'): number {
		if (!this.containerRef || amount <= 0) return 0;
		const detail = { amount, direction, orientation: this.orientation, spaceProvided: 0 };
		this.containerRef.dispatchEvent(new CustomEvent('resizable-request-space', { bubbles: true, detail }));
		return detail.spaceProvided;
	}

	private handleSpaceRequest = (event: Event) => {
		const { detail } = event as CustomEvent<{
			amount: number;
			direction: 'left' | 'right';
			orientation: 'horizontal' | 'vertical';
			spaceProvided: number;
		}>;
		if (detail.orientation !== this.orientation) return;

		const parent = this.findParentPane(event.target as Element);
		if (!parent || !this.containerRef) return;

		const { sizes, mins } = this.measurePanes();
		const newSizes = [...sizes];
		let remaining = detail.amount;

		const indices =
			detail.direction === 'left'
				? Array.from({ length: parent.index }, (_, i) => parent.index - 1 - i)
				: Array.from({ length: this.panes.length - parent.index - 1 }, (_, i) => parent.index + 1 + i);

		for (const i of indices) {
			if (remaining <= 0) break;
			const available = newSizes[i] - mins[i];
			if (available > 0) {
				const take = Math.min(available, remaining);
				newSizes[i] -= take;
				remaining -= take;
			}
		}

		let freed = detail.amount - remaining;
		if (remaining > 0) {
			freed += this.requestParentResize(remaining, detail.direction);
		}

		if (freed > 0) {
			newSizes[parent.index] += freed;
			this.panes.forEach((p, i) => this.setSize(p.id, newSizes[i]));
		}

		detail.spaceProvided += freed;
		event.stopPropagation();
	};

	private handleCollapseRequest = (event: Event) => {
		const { detail } = event as CustomEvent<{
			amount: number;
			expandingPaneIndex: number;
			orientation: 'horizontal' | 'vertical';
			spaceFreed: number;
		}>;
		if (detail.orientation !== this.orientation) return;

		const parent = this.findParentPane(event.target as Element);
		if (!parent || !this.containerRef) return;

		const collapsible = this.panes
			.filter((p) => p.id !== parent.pane.id && p.collapsible && !this.isCollapsed(p.id))
			.map((p) => ({ pane: p, index: this.panes.indexOf(p), min: this.getEffectiveMin(p) }))
			.sort((a, b) => {
				const distDiff = Math.abs(b.index - parent.index) - Math.abs(a.index - parent.index);
				if (distDiff !== 0) return distDiff;
				return detail.expandingPaneIndex > 0 ? a.index - b.index : b.index - a.index;
			});

		let remaining = detail.amount;
		let freed = 0;

		for (const { pane, min } of collapsible) {
			if (remaining <= 0) break;
			this.collapsedPanes.add(pane.id);
			const saved = min - (pane.collapsedSize ?? 0);
			remaining -= saved;
			freed += saved;
		}

		if (freed > 0) {
			this.collapsedPanes = new Set(this.collapsedPanes);
			this.commitLayout();
		}

		if (remaining > 0) {
			freed += this.requestParentCollapse(remaining, parent.index);
		}

		detail.spaceFreed += freed;
		event.stopPropagation();
	};

	private measurePanes(): { sizes: number[]; mins: number[] } {
		const sizes: number[] = [];
		const mins: number[] = [];
		for (const p of this.panes) {
			const el = this.getPaneEl(p.id);
			if (el) {
				const rect = el.getBoundingClientRect();
				sizes.push(this.orientation === 'horizontal' ? rect.width : rect.height);
				mins.push(this.getEffectiveMin(p));
			} else {
				sizes.push(this.getSize(p.id));
				mins.push(p.minSize);
			}
		}
		return { sizes, mins };
	}

	private handleMove = (event: PointerEvent) => {
		if (this.resizingHandleIndex < 0 || !this.containerRef) return;

		const pos = this.orientation === 'horizontal' ? event.clientX : event.clientY;
		const delta = pos - this.lastMovePos;
		this.lastMovePos = pos;

		if (Math.abs(delta) < 1) return;

		const { sizes, mins } = this.measurePanes();
		const newSizes = [...sizes];
		const idx = this.resizingHandleIndex;

		if (delta > 0) {
			let rem = delta;
			for (let i = idx + 1; i < this.panes.length && rem > 0; i++) {
				const avail = newSizes[i] - mins[i];
				if (avail > 0) {
					const take = Math.min(avail, rem);
					newSizes[i] -= take;
					rem -= take;
				}
			}
			if (rem > 0) rem -= this.requestParentResize(rem, 'right');
			newSizes[idx] += delta - rem;
		} else {
			let rem = -delta;
			for (let i = idx; i >= 0 && rem > 0; i--) {
				const avail = newSizes[i] - mins[i];
				if (avail > 0) {
					const take = Math.min(avail, rem);
					newSizes[i] -= take;
					rem -= take;
				}
			}
			if (rem > 0) rem -= this.requestParentResize(rem, 'left');
			if (idx + 1 < this.panes.length) newSizes[idx + 1] += -delta - rem;
		}

		this.panes.forEach((p, i) => this.setSize(p.id, newSizes[i]));
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
