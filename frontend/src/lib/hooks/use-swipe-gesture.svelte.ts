import { onMount } from 'svelte';

export type SwipeDirection = 'up' | 'down' | 'left' | 'right';

export interface SwipeGestureOptions {
	threshold?: number; // Minimum distance in pixels
	velocity?: number; // Minimum velocity in pixels/ms
	timeLimit?: number; // Maximum time for swipe in ms
}

export class SwipeGestureDetector {
	private startX = 0;
	private startY = 0;
	private startTime = 0;
	private onSwipe: (direction: SwipeDirection, details: SwipeDetails) => void;
	private options: Required<SwipeGestureOptions>;
	private cleanupFn: (() => void) | null = null;

	constructor(onSwipe: (direction: SwipeDirection, details: SwipeDetails) => void, options: SwipeGestureOptions = {}) {
		this.onSwipe = onSwipe;
		this.options = {
			threshold: options.threshold ?? 50,
			velocity: options.velocity ?? 0.3,
			timeLimit: options.timeLimit ?? 500
		};

		onMount(() => {
			return () => {
				if (this.cleanupFn) {
					this.cleanupFn();
				}
			};
		});
	}

	setElement(element: HTMLElement | null) {
		// Clean up previous listeners
		if (this.cleanupFn) {
			this.cleanupFn();
			this.cleanupFn = null;
		}

		if (!element) return;

		const handleTouchStart = (e: TouchEvent) => {
			const touch = e.touches[0];
			if (!touch) return;

			this.startX = touch.clientX;
			this.startY = touch.clientY;
			this.startTime = Date.now();
		};

		const handleTouchEnd = (e: TouchEvent) => {
			const touch = e.changedTouches[0];
			if (!touch) return;

			const endX = touch.clientX;
			const endY = touch.clientY;
			const endTime = Date.now();

			const deltaX = endX - this.startX;
			const deltaY = endY - this.startY;
			const deltaTime = endTime - this.startTime;

			// Check if gesture meets time limit
			if (deltaTime > this.options.timeLimit) return;

			const distance = Math.sqrt(deltaX * deltaX + deltaY * deltaY);
			const velocity = distance / deltaTime;

			// Check if gesture meets thresholds
			// More lenient: meet either distance OR velocity threshold
			const meetsDistanceThreshold = distance >= this.options.threshold;
			const meetsVelocityThreshold = velocity >= this.options.velocity;

			if (!meetsDistanceThreshold && !meetsVelocityThreshold) return;

			// Determine primary direction
			const absDeltaX = Math.abs(deltaX);
			const absDeltaY = Math.abs(deltaY);

			let direction: SwipeDirection;
			if (absDeltaX > absDeltaY) {
				direction = deltaX > 0 ? 'right' : 'left';
			} else {
				direction = deltaY > 0 ? 'down' : 'up';
			}

			const details: SwipeDetails = {
				deltaX,
				deltaY,
				distance,
				velocity,
				duration: deltaTime
			};

			this.onSwipe(direction, details);
		};

		element.addEventListener('touchstart', handleTouchStart, { passive: true });
		element.addEventListener('touchend', handleTouchEnd, { passive: true });

		this.cleanupFn = () => {
			element.removeEventListener('touchstart', handleTouchStart);
			element.removeEventListener('touchend', handleTouchEnd);
		};
	}
}

export interface SwipeDetails {
	deltaX: number;
	deltaY: number;
	distance: number;
	velocity: number;
	duration: number;
}
