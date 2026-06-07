import { untrack } from 'svelte';

export type ScrollDirection = 'up' | 'down' | 'idle';

export class ScrollDirectionDetector {
	private _lastScrollY = $state(0);
	private _scrollDirection = $state<ScrollDirection>('idle');
	private _isScrolling = $state(false);
	private scrollThreshold: number;
	private scrollTimeout: ReturnType<typeof setTimeout> | null = null;
	private cleanupFn: (() => void) | null = null;
	private initialized = false;

	constructor(threshold = 10) {
		this.scrollThreshold = threshold;
	}

	private init() {
		if (this.initialized || typeof window === 'undefined') return;
		this.initialized = true;

		this._lastScrollY = window.scrollY;

		const handleScroll = () => {
			const currentScrollY = window.scrollY;
			const scrollDiff = currentScrollY - untrack(() => this._lastScrollY);

			if (Math.abs(scrollDiff) > this.scrollThreshold) {
				const newDirection: ScrollDirection = scrollDiff > 0 ? 'down' : 'up';

				if (untrack(() => this._scrollDirection) !== newDirection) {
					this._scrollDirection = newDirection;
				}
				this._lastScrollY = currentScrollY;
			}

			this._isScrolling = true;

			if (this.scrollTimeout) {
				clearTimeout(this.scrollTimeout);
			}

			this.scrollTimeout = setTimeout(() => {
				this._scrollDirection = 'idle';
				this._isScrolling = false;
			}, 150);
		};

		window.addEventListener('scroll', handleScroll, { passive: true });

		this.cleanupFn = () => {
			window.removeEventListener('scroll', handleScroll);
			if (this.scrollTimeout) {
				clearTimeout(this.scrollTimeout);
			}
		};
	}

	cleanup() {
		if (this.cleanupFn) {
			this.cleanupFn();
			this.cleanupFn = null;
		}
		this.initialized = false;
	}

	get direction() {
		this.init();
		return this._scrollDirection;
	}

	get scrolling() {
		this.init();
		return this._isScrolling;
	}

	get scrollY() {
		this.init();
		return this._lastScrollY;
	}
}
