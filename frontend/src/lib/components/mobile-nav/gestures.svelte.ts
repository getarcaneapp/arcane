import { SwipeGestureDetector, type SwipeDirection } from '$lib/hooks/use-swipe-gesture.svelte';

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
	private lastScrollY = $state(0);
	private lastWheelTime = $state(0);
	private scrollTimeout: ReturnType<typeof setTimeout> | null = null;
	private flickDetectTimeout: ReturnType<typeof setTimeout> | null = null;
	private touchStartY: number | null = null;
	private touchStartX: number | null = null;
	private isInteractiveTouch = false;
	private readonly touchMoveThreshold = 6;
	private readonly scrollThreshold = 10;
	private readonly minScrollDistance = 80;
	private readonly flickVelocityThreshold = 3;

	private navElement: HTMLElement | null = null;
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

	setElement(element: HTMLElement | null) {
		this.navElement = element;
		this.swipeDetector.setElement(element);
	}

	private handleTouchStart = (e: TouchEvent) => {
		if (this.options.menuOpen) return;
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

	private handleTouchMove = (e: TouchEvent) => {
		if (this.options.menuOpen || this.isInteractiveTouch || this.touchStartY === null || this.touchStartX === null) return;
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

	private handleTouchEnd = () => {
		this.touchStartY = null;
		this.touchStartX = null;
		this.isInteractiveTouch = false;
	};

	private handleScroll = (e?: Event) => {
		const currentScrollY = window.scrollY;
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
				if (window.scrollY < this.minScrollDistance) {
					this.handlers.onVisibilityChange(true);
				}
			}, 150);
		}
	};

	private handleWheel = (e: WheelEvent) => {
		e.preventDefault();
		if (this.options.menuOpen || !this.options.scrollToHideEnabled || e.deltaY <= 0) return;

		const now = Date.now();
		const velocity = e.deltaY / Math.max(1, now - this.lastWheelTime);

		if (velocity > this.flickVelocityThreshold) {
			this.handlers.onMenuOpen();
			return;
		}

		this.lastWheelTime = now;

		if (this.flickDetectTimeout) clearTimeout(this.flickDetectTimeout);
		this.flickDetectTimeout = setTimeout(() => {
			this.lastWheelTime = 0;
		}, 200);
	};

	enableTouchGestures() {
		if (typeof window === 'undefined') return () => {};
		if (!this.options.scrollToHideEnabled) return () => {};

		const options = { passive: true, capture: true };
		window.addEventListener('touchstart', this.handleTouchStart, options);
		window.addEventListener('touchmove', this.handleTouchMove, options);
		window.addEventListener('touchend', this.handleTouchEnd, options);

		return () => {
			window.removeEventListener('touchstart', this.handleTouchStart, options);
			window.removeEventListener('touchmove', this.handleTouchMove, options);
			window.removeEventListener('touchend', this.handleTouchEnd, options);
		};
	}

	enableScrollGestures() {
		if (typeof window === 'undefined') return () => {};
		if (!this.options.scrollToHideEnabled || this.options.menuOpen) {
			this.handlers.onVisibilityChange(true);
			return () => {};
		}

		const scrollHandler = (e: Event) => this.handleScroll(e);
		window.addEventListener('scroll', scrollHandler, { passive: true, capture: true });

		return () => {
			window.removeEventListener('scroll', scrollHandler, { capture: true });
			if (this.scrollTimeout) {
				clearTimeout(this.scrollTimeout);
			}
		};
	}

	enableWheelGestures() {
		if (!this.navElement || typeof window === 'undefined') return () => {};

		this.navElement.addEventListener('wheel', this.handleWheel, { passive: false });

		return () => {
			if (this.navElement) {
				this.navElement.removeEventListener('wheel', this.handleWheel);
			}
			if (this.flickDetectTimeout) {
				clearTimeout(this.flickDetectTimeout);
			}
		};
	}

	updateOptions(options: GestureOptions) {
		this.options = options;
	}

	destroy() {
		this.swipeDetector.setElement(null);
		if (this.scrollTimeout) {
			clearTimeout(this.scrollTimeout);
		}
		if (this.flickDetectTimeout) {
			clearTimeout(this.flickDetectTimeout);
		}
	}
}
