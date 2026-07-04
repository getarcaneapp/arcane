import type { Attachment } from 'svelte/attachments';

/**
 * Priority+ overflow state for the header nav: measures a hidden replica row
 * of all items plus the live container, and derives how many items fit before
 * the remainder collapses into the "More" dropdown. ResizeObserver-driven, so
 * locale changes (label widths) and font-size preference changes (root px)
 * re-measure automatically.
 */
export class NavOverflow {
	containerWidth = $state(0);
	itemWidths = $state<number[]>([]);

	/** px reserved for the "More" trigger when not all items fit */
	private reserved: number;
	/** px gap between items (matches the row's gap-* class) */
	private gap: number;

	constructor(reserved = 96, gap = 4) {
		this.reserved = reserved;
		this.gap = gap;
	}

	setItemCount(count: number) {
		if (this.itemWidths.length !== count) {
			this.itemWidths = Array.from({ length: count }, (_, i) => this.itemWidths[i] ?? 0);
		}
	}

	get visibleCount(): number {
		const widths = this.itemWidths;
		if (!this.containerWidth || widths.length === 0 || widths.some((w) => w === 0)) {
			return widths.length;
		}
		let total = 0;
		for (const w of widths) total += w + this.gap;
		if (total - this.gap <= this.containerWidth) return widths.length;

		let used = this.reserved + this.gap;
		let count = 0;
		for (const w of widths) {
			used += w + this.gap;
			if (used > this.containerWidth) break;
			count++;
		}
		return count;
	}
}

/** Attachment factory: report the element's width now and whenever it resizes. */
export function observeWidth(onWidth: (width: number) => void): Attachment<HTMLElement> {
	return (node) => {
		const report = () => onWidth(node.offsetWidth);
		const observer = new ResizeObserver(report);
		observer.observe(node);
		report();
		return () => observer.disconnect();
	};
}
