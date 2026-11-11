import { EditorView, ViewPlugin } from '@codemirror/view';

// Focusing an element scrolls it into view. When the page scrolls a full-height
// editor (phones), that lands on line 1, which iOS Safari does on the first tap.
// Undo page scrolls briefly after a tap focuses the editor, then reveal the caret.
const tapFocusWindowMs = 1000;
const restoreWindowMs = 600;
// Anywhere on the page, these mean the user took over.
const userInputEvents = ['touchstart', 'pointerdown', 'wheel', 'keydown'] as const;

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
		const doc = this.view.dom.ownerDocument;
		doc.addEventListener('scroll', this.restore, { capture: true, passive: true });
		for (const type of userInputEvents) doc.addEventListener(type, this.release, { capture: true, passive: true });
		this.timer = setTimeout(this.finish, restoreWindowMs);
	}

	release = () => {
		clearTimeout(this.timer);
		const doc = this.view.dom.ownerDocument;
		doc.removeEventListener('scroll', this.restore, { capture: true });
		for (const type of userInputEvents) doc.removeEventListener(type, this.release, { capture: true });
		this.positions = [];
		this.restored = false;
	};

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
		blur() {
			this.release();
		}
	}
});
