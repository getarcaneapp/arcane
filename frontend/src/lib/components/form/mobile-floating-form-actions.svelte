<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { getEffectiveNavigationSettings } from '$lib/utils/navigation.utils';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';
	import { IsTablet } from '$lib/hooks/is-tablet.svelte.js';
	import settingsStore from '$lib/stores/config-store';
	import { cn } from '$lib/utils';

	interface Props {
		hasChanges: boolean | null;
		isLoading: boolean;
		onSave: () => void | Promise<void>;
		onReset: () => void;
	}

	let { hasChanges, isLoading, onSave, onReset }: Props = $props();

	const isMobile = new IsMobile();
	const isTablet = new IsTablet();
	const isGlassEnabled = $derived.by(() => $settingsStore?.glassEffectEnabled ?? false);
	const navigationSettings = $derived(getEffectiveNavigationSettings());
	const navigationMode = $derived(navigationSettings.mode);
	const scrollToHideEnabled = $derived(navigationSettings.scrollToHide);

	// Track mobile nav visibility for FAB positioning
	let mobileNavVisible = $state(true);

	// Monitor mobile nav visibility when scroll-to-hide is enabled
	$effect(() => {
		if (typeof window === 'undefined') return;
		if (!scrollToHideEnabled || !(isMobile.current || isTablet.current)) {
			mobileNavVisible = true;
			return;
		}

		// Check the mobile nav element's transform to determine visibility
		const checkNavVisibility = () => {
			const navElement = document.querySelector('[data-testid="mobile-floating-nav"], [data-testid="mobile-docked-nav"]');
			if (!navElement) {
				mobileNavVisible = true;
				return;
			}

			const style = window.getComputedStyle(navElement);
			const transform = style.transform;
			const opacity = parseFloat(style.opacity);

			// Check if nav is translated away or has low opacity
			if (transform !== 'none' && transform.includes('matrix')) {
				const matrix = transform.match(/matrix.*\((.+)\)/);
				if (matrix) {
					const values = matrix[1].split(', ');
					const translateY = parseFloat(values[5] || '0');
					// If translateY is positive (moved down), nav is hidden
					mobileNavVisible = translateY === 0 && opacity > 0.5;
				}
			} else {
				mobileNavVisible = opacity > 0.5;
			}
		};

		// Initial check
		checkNavVisibility();

		// Use MutationObserver to watch for style changes on nav
		const observer = new MutationObserver(checkNavVisibility);
		const navElement = document.querySelector('[data-testid="mobile-floating-nav"], [data-testid="mobile-docked-nav"]');

		if (navElement) {
			observer.observe(navElement, {
				attributes: true,
				attributeFilter: ['style', 'class']
			});
		}

		// Also check on scroll as a fallback
		const handleScroll = () => {
			requestAnimationFrame(checkNavVisibility);
		};

		window.addEventListener('scroll', handleScroll, { passive: true });

		return () => {
			observer.disconnect();
			window.removeEventListener('scroll', handleScroll);
		};
	});
</script>

{#if isMobile.current || isTablet.current}
	<div
		class="fixed right-4 z-50 flex flex-col gap-3 transition-all duration-300 ease-out sm:hidden"
		style="bottom: {scrollToHideEnabled && !mobileNavVisible
			? '1rem'
			: 'calc(var(--mobile-' +
				navigationMode +
				'-nav-offset, ' +
				(navigationMode === 'docked' ? 'calc(3.5rem + env(safe-area-inset-bottom))' : '6rem') +
				') + 1rem)'};"
	>
		{#if hasChanges}
			<ArcaneButton
				action="restart"
				tone="outline"
				size="lg"
				onclick={onReset}
				disabled={isLoading}
				class={cn('size-14 rounded-full border-2 shadow-lg', isGlassEnabled ? 'bg-background/80 backdrop-blur-md' : 'bg-card')}
				showLabel={false}
			/>
		{/if}

		<ArcaneButton
			action="save"
			onclick={onSave}
			disabled={isLoading || !hasChanges}
			loading={isLoading}
			size="lg"
			class="size-14 rounded-full shadow-lg"
			showLabel={false}
		/>

		<!-- Status indicator for mobile -->
		{#if hasChanges}
			<div class="absolute -top-2 -left-2 size-3 animate-pulse rounded-full bg-orange-500"></div>
		{/if}
	</div>
{/if}
