/**
 * Detects if the device has touch capability.
 * 
 * Note: Some hybrid devices (laptops with touchscreens) will be detected as touch devices,
 * which is correct as they support both touch and pointer interactions.
 */
export class IsTouchDevice {
	current = $state(false);

	constructor() {
		// Check on initialization
		this.current = this.detectTouch();

		// Re-check if media query changes (for devices that can switch modes)
		$effect(() => {
			this.current = this.detectTouch();
		});
	}

	private detectTouch(): boolean {
		// Check if running in browser
		if (typeof window === 'undefined') return false;

		// Multiple checks for better accuracy across browsers
		const hasTouchPoints = 'maxTouchPoints' in navigator && navigator.maxTouchPoints > 0;
		const hasTouchStart = 'ontouchstart' in window;
		const hasTouchEvent = 'DocumentTouch' in window && 'ontouchstart' in document.documentElement;
		const hasPointerCoarse = window.matchMedia?.('(pointer: coarse)').matches;

		return hasTouchPoints || hasTouchStart || hasTouchEvent || hasPointerCoarse;
	}
}

