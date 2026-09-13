import { SwipeGestureDetector, type SwipeDirection } from '#lib/hooks/use-swipe-gesture.svelte.js';
import { on } from 'svelte/events';

export interface GestureHandlers {
	onMenuOpen: () => void;
	onVisibilityChange: (visible: boolean) => void;
}

export interface GestureOptions {
	scrollToHideEnabled: boolean;
	menuOpen: boolean;
}

export class MobileNavGestures {
	private swipeDetector: SwipeGestureDetector;
	private lastScrollY = 0;
	private lastWheelTime = 0;
	private scrollTimeout: ReturnType<typeof setTimeout> | null = null;
	private flickDetectTimeout: ReturnType<typeof setTimeout> | null = null;
	private touchStartY: number | null = null;
	private touchStartX: number | null = null;
	private isInteractiveTouch = false;
	private readonly touchMoveThreshold = 6;
	private readonly scrollThreshold = 10;
	private readonly minScrollDistance = 80;
	private readonly flickVelocityThreshold = 3;

	private handlers: GestureHandlers;
	private options: GestureOptions;

	constructor(handlers: GestureHandlers, options: GestureOptions) {
		this.handlers = handlers;
		this.options = options;

		this.swipeDetector = new SwipeGestureDetector(
			(direction: SwipeDirection) => {
				if (direction === 'up') {
					this.handlers.onMenuOpen();
				}
			},
			{
				threshold: 20,
				velocity: 0.1,
				timeLimit: 1000
			}
		);
	}

	handleTouchStart = (e: TouchEvent) => {
		this.handleTouchEnd();
		if (!this.options.scrollToHideEnabled || this.options.menuOpen) return;
		const t = e.touches?.[0];
		if (!t) return;
		const target = e.target as HTMLElement | null;

		if (target && target.closest && target.closest('button, a, input, select, textarea, [role="button"], [contenteditable]')) {
			this.isInteractiveTouch = true;
			this.touchStartY = null;
			this.touchStartX = null;
			return;
		}
		this.isInteractiveTouch = false;
		this.touchStartY = t.clientY;
		this.touchStartX = t.clientX;
	};

	handleTouchMove = (e: TouchEvent) => {
		if (
			!this.options.scrollToHideEnabled ||
			this.options.menuOpen ||
			this.isInteractiveTouch ||
			this.touchStartY === null ||
			this.touchStartX === null
		)
			return;
		const t = e.touches?.[0];
		if (!t) return;
		const deltaY = t.clientY - this.touchStartY;
		const deltaX = t.clientX - this.touchStartX;

		if (Math.abs(deltaY) < this.touchMoveThreshold && Math.abs(deltaX) < this.touchMoveThreshold) return;

		// If horizontal movement is dominant, ignore this touch sequence
		if (Math.abs(deltaX) > Math.abs(deltaY)) {
			return;
		}

		if (deltaY < 0) {
			this.handlers.onVisibilityChange(false);
		} else {
			this.handlers.onVisibilityChange(true);
		}

		this.touchStartY = t.clientY;
		this.touchStartX = t.clientX;
	};

	handleTouchEnd = () => {
		this.touchStartY = null;
		this.touchStartX = null;
		this.isInteractiveTouch = false;
	};

	handleScroll = (e: Event) => {
		const currentScrollY = window.scrollY;
		if (!this.options.scrollToHideEnabled || this.options.menuOpen) {
			this.lastScrollY = currentScrollY;
			return;
		}
		const prevScrollY = this.lastScrollY;
		const scrollDiff = currentScrollY - prevScrollY;

		if (e && e.target && e.target !== document && e.target !== window) {
			const target = e.target as HTMLElement;
			if (target.scrollLeft !== undefined && target.scrollLeft > 0) {
				return;
			}
		}

		if (this.scrollTimeout) {
			clearTimeout(this.scrollTimeout);
			this.scrollTimeout = null;
		}

		const scrollHeight = document.documentElement.scrollHeight;
		const clientHeight = document.documentElement.clientHeight;
		const atBottom = currentScrollY + clientHeight >= scrollHeight - 5;

		if (scrollDiff < 0 && !atBottom) {
			this.handlers.onVisibilityChange(true);
			this.lastScrollY = currentScrollY;
		} else if (scrollDiff > this.scrollThreshold && currentScrollY > this.minScrollDistance && !atBottom) {
			this.handlers.onVisibilityChange(false);
			this.lastScrollY = currentScrollY;
		} else if (Math.abs(scrollDiff) > this.scrollThreshold) {
			this.lastScrollY = currentScrollY;
		}

		if (!atBottom) {
			this.scrollTimeout = setTimeout(() => {
				this.scrollTimeout = null;
				if (this.options.scrollToHideEnabled && !this.options.menuOpen && window.scrollY < this.minScrollDistance) {
					this.handlers.onVisibilityChange(true);
				}
			}, 150);
		}
	};

	private handleWheel = (e: WheelEvent) => {
		e.preventDefault();
		if (this.options.menuOpen || !this.options.scrollToHideEnabled || e.deltaY <= 0) return;

		const now = performance.now();
		const velocity = e.deltaY / Math.max(1, now - this.lastWheelTime);

		if (velocity > this.flickVelocityThreshold) {
			this.handlers.onMenuOpen();
			return;
		}

		this.lastWheelTime = now;

		if (this.flickDetectTimeout) clearTimeout(this.flickDetectTimeout);
		this.flickDetectTimeout = setTimeout(() => {
			this.lastWheelTime = 0;
			this.flickDetectTimeout = null;
		}, 200);
	};

	reset() {
		this.handleTouchEnd();
		this.lastWheelTime = 0;
		if (this.scrollTimeout) clearTimeout(this.scrollTimeout);
		if (this.flickDetectTimeout) clearTimeout(this.flickDetectTimeout);
		this.scrollTimeout = null;
		this.flickDetectTimeout = null;
	}

	attach = (element: HTMLElement) => {
		this.lastScrollY = window.scrollY;
		this.swipeDetector.setElement(element);
		const removeWheelListener = on(element, 'wheel', this.handleWheel, { passive: false });

		return () => {
			removeWheelListener();
			this.swipeDetector.setElement(null);
			this.reset();
		};
	};
}
