import { ScrollDirectionDetector } from './use-scroll-direction.svelte';
import { SwipeGestureDetector, type SwipeDirection } from './use-swipe-gesture.svelte';

export interface MobileNavInteractionOptions {
	// Scroll detection options
	scrollThreshold?: number;
	scrollMinDistance?: number;
	scrollTopThreshold?: number;
	scrollChangeThreshold?: number;

	// Tap detection options
	tapDebounceTimeout?: number;

	// Swipe detection options
	swipeThreshold?: number;
	swipeVelocity?: number;
	swipeTimeLimit?: number;

	// Touch handling options
	touchEndDelay?: number;
	menuOpenRestoreDelay?: number;
	overlayFadeOutDelay?: number;
	wheelThreshold?: number;
}

export interface MobileNavInteractionCallbacks {
	onVisibilityChange: (visible: boolean) => void;
	onMenuOpen: () => void;
	onScrollDirectionChange?: (direction: string, scrollY: number) => void;
	shouldPreventTouch?: (menuOpen: boolean) => boolean;
}

export interface MobileNavInteractionState {
	menuOpen: boolean;
	scrollToHideEnabled: boolean;
	visible: boolean;
}

export class MobileNavInteractionManager {
	private readonly scrollDetector: ScrollDirectionDetector;
	private readonly swipeDetector: SwipeGestureDetector;

	private readonly options: Required<MobileNavInteractionOptions>;
	private readonly callbacks: MobileNavInteractionCallbacks;
	private state: MobileNavInteractionState;

	private navElement: HTMLElement | null = null;
	private lastScrollDirection: string | null = null;
	private lastScrollY = 0;

	// Touch handling state
	private touchStartTarget: HTMLElement | null = null;
	private isInteractiveTouch = false;

	// Cleanup functions
	private cleanupFunctions: (() => void)[] = [];

	constructor(callbacks: MobileNavInteractionCallbacks, options: MobileNavInteractionOptions = {}) {
		this.callbacks = callbacks;
		this.options = {
			scrollThreshold: options.scrollThreshold ?? 15,
			scrollMinDistance: options.scrollMinDistance ?? 100,
			scrollTopThreshold: options.scrollTopThreshold ?? 100,
			scrollChangeThreshold: options.scrollChangeThreshold ?? 50,
			tapDebounceTimeout: options.tapDebounceTimeout ?? 300,
			swipeThreshold: options.swipeThreshold ?? 20,
			swipeVelocity: options.swipeVelocity ?? 0.1,
			swipeTimeLimit: options.swipeTimeLimit ?? 1000,
			touchEndDelay: options.touchEndDelay ?? 150,
			menuOpenRestoreDelay: options.menuOpenRestoreDelay ?? 50,
			overlayFadeOutDelay: options.overlayFadeOutDelay ?? 200,
			wheelThreshold: options.wheelThreshold ?? 10
		};

		this.state = {
			menuOpen: false,
			scrollToHideEnabled: false,
			visible: true
		};

		// Initialize detectors directly in constructor
		this.scrollDetector = new ScrollDirectionDetector(this.options.scrollThreshold);

		this.swipeDetector = new SwipeGestureDetector(
			(direction: SwipeDirection) => {
				if (direction === 'up') {
					this.callbacks.onMenuOpen();
				}
			},
			{
				threshold: this.options.swipeThreshold,
				velocity: this.options.swipeVelocity,
				timeLimit: this.options.swipeTimeLimit
			}
		);
	}

	public updateState(newState: Partial<MobileNavInteractionState>) {
		this.state = { ...this.state, ...newState };
	}

	public resetVisibility() {
		this.state.visible = true;
		this.callbacks.onVisibilityChange(true);
	}

	public setupElement(element: HTMLElement | null) {
		// Cleanup previous setup
		this.cleanup();

		this.navElement = element;

		if (!element) return;

		// Setup swipe detection target
		this.swipeDetector.setElement(element);

		// Setup touch event handlers
		this.setupTouchHandlers(element);

		// Setup mouse event handlers for desktop/pointer devices
		this.setupMouseHandlers(element);
	}

	private setupTouchHandlers(element: HTMLElement) {
		const handleTouchStart = (e: TouchEvent) => {
			const shouldPreventTouch = this.callbacks.shouldPreventTouch?.(this.state.menuOpen);
			if (shouldPreventTouch) return;

			// Store touch start target and check if it's interactive
			this.touchStartTarget = e.target as HTMLElement;
			this.isInteractiveTouch = !!this.touchStartTarget.closest('button, a, [role="button"], input, select, textarea');

			// Don't prevent default or lock scrolling - let native behavior work
			// The CSS touch-action: pan-y will handle this properly
		};

		const handleTouchMove = (e: TouchEvent) => {
			const shouldPreventTouch = this.callbacks.shouldPreventTouch?.(this.state.menuOpen);
			if (shouldPreventTouch) return;

			// Don't prevent default - let native scrolling work
			// Only track if we moved off an interactive element
			if (this.isInteractiveTouch) {
				const touch = e.touches[0];
				if (!touch) return;

				const target = document.elementFromPoint(touch.clientX, touch.clientY) as HTMLElement;
				const stillOnInteractive = target && target.closest('button, a, [role="button"], input, select, textarea');

				if (!stillOnInteractive) {
					// Moved off interactive element, treat as gesture
					this.isInteractiveTouch = false;
				}
			}
		};

		const handleTouchEnd = () => {
			// Reset touch state
			this.touchStartTarget = null;
			this.isInteractiveTouch = false;
		};

		element.addEventListener('touchstart', handleTouchStart, { passive: true, capture: false });
		element.addEventListener('touchmove', handleTouchMove, { passive: true, capture: false });
		element.addEventListener('touchend', handleTouchEnd, { passive: true, capture: false });

		// Store cleanup functions
		this.cleanupFunctions.push(() => {
			element.removeEventListener('touchstart', handleTouchStart);
			element.removeEventListener('touchmove', handleTouchMove);
			element.removeEventListener('touchend', handleTouchEnd);
		});
	}

	private setupMouseHandlers(element: HTMLElement) {
		const handleWheel = (e: WheelEvent) => {
			// Don't prevent default - let page scroll naturally
			// Only respond to downward gestures on the navbar itself to open menu
			if (e.deltaY > this.options.wheelThreshold) {
				this.callbacks.onMenuOpen();
			}
		};

		const handleMouseEnter = () => {
			// Don't lock scrolling or manipulate pointer events
			// Just listen for wheel events on the navbar
			element.addEventListener('wheel', handleWheel, { passive: true });
		};

		const handleMouseLeave = () => {
			// Clean up wheel listener
			element.removeEventListener('wheel', handleWheel);
		};

		element.addEventListener('mouseenter', handleMouseEnter);
		element.addEventListener('mouseleave', handleMouseLeave);

		// Store cleanup functions
		this.cleanupFunctions.push(() => {
			element.removeEventListener('mouseenter', handleMouseEnter);
			element.removeEventListener('mouseleave', handleMouseLeave);
			element.removeEventListener('wheel', handleWheel);
		});
	}

	public handleScrollEffect(direction: string, scrollY: number) {
		// Only update when scroll direction or position changes significantly
		if (
			(direction !== this.lastScrollDirection || Math.abs(scrollY - this.lastScrollY) > this.options.scrollChangeThreshold) &&
			this.state.scrollToHideEnabled
		) {
			this.lastScrollDirection = direction;
			this.lastScrollY = scrollY;

			// Call optional callback
			this.callbacks.onScrollDirectionChange?.(direction, scrollY);

			// Update visibility state based on scroll behavior
			if (direction === 'down' && scrollY > this.options.scrollMinDistance) {
				this.state.visible = false;
				this.callbacks.onVisibilityChange(false);
			} else if (direction === 'up') {
				this.state.visible = true;
				this.callbacks.onVisibilityChange(true);
			} else if (direction === 'idle' && scrollY <= this.options.scrollTopThreshold) {
				this.state.visible = true;
				this.callbacks.onVisibilityChange(true);
			}
		}
	}

	public handleMenuStateChange(previousMenuOpen: boolean, currentMenuOpen: boolean) {
		// If menu was open and is now closed, make navigation bar visible
		if (previousMenuOpen && !currentMenuOpen) {
			this.state.visible = true;
			this.callbacks.onVisibilityChange(true);
		}
	}

	public cleanup() {
		// Cleanup all event listeners
		this.cleanupFunctions.forEach((fn) => fn());
		this.cleanupFunctions = [];

		// Cleanup scroll detector
		this.scrollDetector.cleanup();

		// Clean up any remaining styles
		if (document.body) {
			document.body.style.pointerEvents = '';
		}

		if (this.navElement) {
			this.navElement.style.pointerEvents = '';
		}
	}

	// Getters for accessing detectors
	public get scrollDirection() {
		return this.scrollDetector.direction;
	}

	public get scrollY() {
		return this.scrollDetector.scrollY;
	}
}

// Convenience function for Svelte components
export function createMobileNavInteractions(callbacks: MobileNavInteractionCallbacks, options: MobileNavInteractionOptions = {}) {
	return new MobileNavInteractionManager(callbacks, options);
}
