<script lang="ts">
	import { page } from '$app/state';
	import type { MobileNavigationSettings } from '$lib/config/navigation-config';
	import { getAvailableMobileNavItems } from '$lib/config/navigation-config';
	import MobileNavItem from './mobile-nav-item.svelte';
	import MobileNavMenuButton from './mobile-nav-menu-button.svelte';
	import { cn } from '$lib/utils';
	import MobileNavSheet from './mobile-nav-sheet.svelte';
	import { m } from '$lib/paraglide/messages';
	import { MobileNavGestures } from './gestures.svelte';
	import './styles.css';

	let {
		navigationSettings,
		user = null,
		versionInformation = null,
		class: className = ''
	}: {
		navigationSettings: MobileNavigationSettings;
		user?: any;
		versionInformation?: any;
		class?: string;
	} = $props();

	const pinnedItems = $derived.by(() => {
		if (!navigationSettings?.pinnedItems) return [];
		const availableItems = getAvailableMobileNavItems();
		return navigationSettings.pinnedItems
			.map((url) => availableItems.find((item) => item.url === url))
			.filter((item) => item !== undefined);
	});

	const currentPath = $derived(page.url.pathname);
	const showLabels = $derived(navigationSettings?.showLabels ?? true);
	const scrollToHideEnabled = $derived(navigationSettings?.scrollToHide ?? true);
	const mode = $derived(navigationSettings?.mode ?? 'floating');

	const leftItems = $derived(pinnedItems.slice(0, Math.floor(pinnedItems.length / 2)));
	const rightItems = $derived(pinnedItems.slice(Math.floor(pinnedItems.length / 2)));

	let visible = $state(true);
	let menuOpen = $state(false);
	let navElement: HTMLElement;

	const gestures = new MobileNavGestures(
		{
			onMenuOpen: () => (menuOpen = true),
			onVisibilityChange: (isVisible) => (visible = isVisible)
		},
		{
			scrollToHideEnabled: false,
			menuOpen: false
		}
	);

	// Update gesture options when reactive values change
	$effect(() => {
		gestures.updateOptions({
			scrollToHideEnabled,
			menuOpen
		});
	});

	// Enable touch gestures
	$effect(() => {
		scrollToHideEnabled;
		return gestures.enableTouchGestures();
	});

	// Enable scroll gestures
	$effect(() => {
		scrollToHideEnabled;
		menuOpen;
		return gestures.enableScrollGestures();
	});

	// Enable wheel gestures on nav element
	$effect(() => {
		scrollToHideEnabled;
		menuOpen;
		if (navElement) {
			gestures.setElement(navElement);
			return gestures.enableWheelGestures();
		}
	});

	// Show nav when menu closes
	$effect(() => {
		if (!menuOpen) {
			visible = true;
		}
	});

	// Keep page padding in sync with nav height via CSS variables
	$effect(() => {
		if (!navElement || typeof document === 'undefined') return;

		const root = document.documentElement;
		const cssVarName = mode === 'floating' ? '--mobile-floating-nav-offset' : '--mobile-docked-nav-offset';

		const applyOffset = () => {
			if (mode === 'floating') {
				const rect = navElement.getBoundingClientRect();
				const computed = window.getComputedStyle(navElement);
				const bottomGap = parseFloat(computed.bottom || '0') || 0;
				const offset = rect.height + bottomGap;
				root.style.setProperty(cssVarName, `${offset}px`);
			} else {
				root.style.setProperty(cssVarName, `${navElement.offsetHeight}px`);
			}
		};

		applyOffset();

		let observer: ResizeObserver | null = null;
		if (typeof ResizeObserver !== 'undefined') {
			observer = new ResizeObserver(() => applyOffset());
			observer.observe(navElement);
		}

		const handleResize = mode === 'floating' ? () => applyOffset() : null;
		if (handleResize && typeof window !== 'undefined') {
			window.addEventListener('resize', handleResize);
			window.visualViewport?.addEventListener('resize', handleResize);
		}

		return () => {
			observer?.disconnect();
			if (handleResize && typeof window !== 'undefined') {
				window.removeEventListener('resize', handleResize);
				window.visualViewport?.removeEventListener('resize', handleResize);
			}
			root.style.removeProperty(cssVarName);
		};
	});

	// Mode-specific styles
	const navClasses = $derived(
		cn(
			'mobile-nav-base',
			mode === 'floating' ? 'mobile-nav-floating' : 'mobile-nav-docked',
			mode === 'floating' ? 'fixed left-1/2 z-50 -translate-x-1/2 transform' : 'fixed bottom-0 left-0 right-0 z-50 gap-2',
			'bg-background/60 border-border/30 backdrop-blur-xl',
			'shadow-sm select-none transition-all duration-300 ease-out',
			'flex items-center',
			mode === 'floating'
				? cn(
						'rounded-3xl border',
						// On very small screens, tighten spacing so labeled items + center action fit.
						showLabels ? 'gap-1.5 px-2.5 py-1.5 sm:gap-2 sm:px-3 sm:py-2' : 'gap-2.5 px-3.5 py-2 sm:gap-3 sm:px-4 sm:py-2.5'
					)
				: cn('border-t border-border/50 justify-around', showLabels ? 'px-4 pt-2 pb-4' : 'px-4 pt-2.5 pb-4'),
			visible
				? mode === 'floating'
					? 'translate-y-0 scale-100 opacity-100'
					: 'translate-y-0 opacity-100'
				: mode === 'floating'
					? 'translate-y-full scale-95 opacity-0'
					: 'translate-y-full opacity-0',
			className
		)
	);

	const ariaLabel = $derived(mode === 'docked' ? m.mobile_navigation() : 'Mobile navigation');
	const testId = $derived(mode === 'floating' ? 'mobile-floating-nav' : 'mobile-docked-nav');
</script>

<nav bind:this={navElement} class={navClasses} data-testid={testId} aria-label={ariaLabel}>
	<!-- Left side items -->
	{#each leftItems as item (item.url)}
		<MobileNavItem {item} {showLabels} active={currentPath === item.url || currentPath.startsWith(item.url + '/')} />
	{/each}

	<!-- Center action button -->
	<MobileNavMenuButton {showLabels} onclick={() => (menuOpen = true)} />

	{#each rightItems as item (item.url)}
		<MobileNavItem {item} {showLabels} active={currentPath === item.url || currentPath.startsWith(item.url + '/')} />
	{/each}

	{#if pinnedItems.length === 0}
		<MobileNavMenuButton {showLabels} onclick={() => (menuOpen = true)} />
	{/if}
</nav>

<MobileNavSheet bind:open={menuOpen} {user} {versionInformation} navigationMode={mode} />
