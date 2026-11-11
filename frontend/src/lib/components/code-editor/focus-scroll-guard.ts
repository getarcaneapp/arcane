import { EditorView, ViewPlugin } from '@codemirror/view';

// Focusing an element scrolls it into view. When the page scrolls a full-height
// editor (phones), that lands on line 1, which iOS Safari does on the first tap.
// Undo page scrolls briefly after a tap focuses the editor, then reveal the caret.
const tapFocusWindowMs = 1000;
const restoreWindowMs = 600;

type ScrollPosition = { element: Element; top: number; left: number };

class FocusScrollGuard {
	private readonly view: EditorView;
	private lastTouchEnd = 0;
	private positions: ScrollPosition[] = [];
	private restored = false;
	private timer: ReturnType<typeof setTimeout> | undefined;

	constructor(view: EditorView) {
		this.view = view;
	}

	destroy() {
		this.release();
	}

	touchEnded() {
		this.lastTouchEnd = Date.now();
	}

	focused() {
		if (Date.now() - this.lastTouchEnd > tapFocusWindowMs) return;
		this.release();
		for (let element = this.view.dom.parentElement; element; element = element.parentElement) {
			this.positions.push({ element, top: element.scrollTop, left: element.scrollLeft });
		}
		this.view.dom.ownerDocument.addEventListener('scroll', this.restore, { capture: true, passive: true });
		this.timer = setTimeout(this.finish, restoreWindowMs);
	}

	release() {
		clearTimeout(this.timer);
		this.view.dom.ownerDocument.removeEventListener('scroll', this.restore, { capture: true });
		this.positions = [];
		this.restored = false;
	}

	private restore = () => {
		for (const { element, top, left } of this.positions) {
			if (element.scrollTop === top && element.scrollLeft === left) continue;
			element.scrollTo({ top, left, behavior: 'instant' });
			this.restored = true;
		}
	};

	private finish = () => {
		const restored = this.restored;
		this.release();
		if (!restored) return;
		this.view.dispatch({ effects: EditorView.scrollIntoView(this.view.state.selection.main.head, { y: 'nearest' }) });
	};
}

export const focusScrollGuard = ViewPlugin.fromClass(FocusScrollGuard, {
	eventObservers: {
		touchend() {
			this.touchEnded();
		},
		focus() {
			this.focused();
		},
		// The user took over; let their scrolling and typing move the page.
		touchstart() {
			this.release();
		},
		wheel() {
			this.release();
		},
		keydown() {
			this.release();
		}
	}
});
