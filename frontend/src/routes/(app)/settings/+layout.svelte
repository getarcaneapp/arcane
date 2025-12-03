<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { setContext } from 'svelte';
	import { Button } from '$lib/components/ui/button';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import SaveIcon from '@lucide/svelte/icons/save';
	import RotateCcwIcon from '@lucide/svelte/icons/rotate-ccw';
	import { useSidebar } from '$lib/components/ui/sidebar/context.svelte.js';
	import { m } from '$lib/paraglide/messages';
	import settingsStore from '$lib/stores/config-store';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';
	import { IsTablet } from '$lib/hooks/is-tablet.svelte.js';
	import { getEffectiveNavigationSettings } from '$lib/utils/navigation.utils';

	interface Props {
		children: import('svelte').Snippet;
	}

	let { children }: Props = $props();

	let currentPath = $derived(page.url.pathname);
	let isSubPage = $derived(currentPath !== '/settings');
	let currentPageName = $derived(page.url.pathname.split('/').pop() || 'settings');

	const sidebar = useSidebar();
	const isMobile = new IsMobile();
	const isTablet = new IsTablet();
	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);
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

	// Calculate left position based on sidebar state to match sidebar spacing system
	// Uses the same CSS variables and spacing as the sidebar component
	const leftPosition = $derived(() => {
		const margin = '1rem'; // Standard spacing-4 equivalent

		// On mobile, use standard margin without sidebar offset
		if (isMobile.current) {
			return margin;
		}

		if (sidebar.state === 'expanded') {
			// Full sidebar width + standard margin
			return `calc(var(--sidebar-width) + ${margin})`;
		} else {
			// For floating variant with icon collapsible:
			// sidebar-width-icon + spacing(4) + 2px padding + standard margin
			// This matches the exact calculation from sidebar.svelte line 84
			return `calc(var(--sidebar-width-icon) + 1rem + 2px + ${margin})`;
		}
	});

	let pageTitle = $derived(() => {
		switch (currentPageName) {
			case 'general':
				return m.general_title();
			case 'docker':
				return m.docker_title();
			case 'security':
				return m.security_title();
			case 'users':
				return m.users_title();
			case 'navigation':
				return m.navigation_title();
			default:
				return m.sidebar_settings();
		}
	});

	// Create a custom event to communicate with form components
	let formState = $state({
		hasChanges: false,
		isLoading: false,
		saveFunction: null as (() => Promise<void>) | null,
		resetFunction: null as (() => void) | null
	});

	// Set context so forms can update the header state
	setContext('settingsFormState', formState);

	function goBackToSettings() {
		goto('/settings');
	}

	async function handleSave() {
		if (formState.saveFunction) {
			await formState.saveFunction();
		}
	}
</script>

{#if isSubPage}
	<div
		class="bg-background/95 fixed top-4 z-5 rounded-lg border shadow-lg backdrop-blur-md transition-all duration-200"
		style="left: {leftPosition()}; right: 1rem;"
	>
		<div class="px-4 py-3">
			<div class="flex items-center justify-between gap-4">
				<div class="flex min-w-0 items-center gap-2">
					<Button
						variant="ghost"
						size="sm"
						onclick={goBackToSettings}
						class="text-muted-foreground hover:text-foreground shrink-0 gap-2"
					>
						<ArrowLeftIcon class="size-4" />
						<span class="hidden sm:inline">Back</span>
					</Button>

					<nav class="flex min-w-0 items-center gap-2 text-sm">
						<Button
							variant="ghost"
							size="sm"
							onclick={goBackToSettings}
							class="text-muted-foreground hover:text-foreground shrink-0 gap-2"
						>
							<SettingsIcon class="size-4" />
							<span>Settings</span>
						</Button>
						<ChevronRightIcon class="text-muted-foreground size-4 shrink-0" />
						<span class="text-foreground truncate font-medium">{pageTitle()}</span>
					</nav>
				</div>

				<!-- Save Section - Desktop only -->
				<div class="hidden shrink-0 items-center gap-3 sm:flex">
					{#if !isReadOnly}
						{#if formState.hasChanges}
							<span class="text-xs text-orange-600 dark:text-orange-400"> Unsaved changes </span>
						{:else if !formState.hasChanges && formState.saveFunction}
							<span class="text-xs text-green-600 dark:text-green-400"> All changes saved </span>
						{/if}

						{#if formState.hasChanges && formState.resetFunction}
							<Button
								variant="outline"
								size="sm"
								onclick={() => formState.resetFunction && formState.resetFunction()}
								disabled={formState.isLoading}
								class="gap-2"
							>
								<RotateCcwIcon class="size-4" />
								<span class="hidden sm:inline">{m.common_reset()}</span>
							</Button>
						{/if}

						<Button
							onclick={handleSave}
							disabled={formState.isLoading || !formState.hasChanges || !formState.saveFunction}
							size="sm"
							class="min-w-[80px] gap-2"
						>
							{#if formState.isLoading}
								<div class="border-background size-4 animate-spin rounded-full border-2 border-t-transparent"></div>
								<span class="hidden sm:inline">{m.common_saving()}</span>
							{:else}
								<SaveIcon class="size-4" />
								<span class="hidden sm:inline">{m.common_save()}</span>
							{/if}
						</Button>
					{/if}
				</div>
			</div>
		</div>
	</div>
{/if}

<div class="settings-container">
	<div class="settings-content w-full max-w-none" class:pt-20={isSubPage}>
		{@render children()}
	</div>
</div>

<!-- Mobile Floating Action Buttons -->
{#if isSubPage && !isReadOnly}
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
		{#if formState.hasChanges && formState.resetFunction}
			<Button
				variant="outline"
				size="lg"
				onclick={() => formState.resetFunction && formState.resetFunction()}
				disabled={formState.isLoading}
				class="bg-background/80 size-14 rounded-full border-2 shadow-lg backdrop-blur-md"
			>
				<RotateCcwIcon class="size-5" />
			</Button>
		{/if}

		<Button
			onclick={handleSave}
			disabled={formState.isLoading || !formState.hasChanges || !formState.saveFunction}
			size="lg"
			class="bg-primary text-primary-foreground hover:bg-primary/90 disabled:bg-muted disabled:text-muted-foreground size-14 rounded-full shadow-lg"
		>
			{#if formState.isLoading}
				<div class="border-background size-5 animate-spin rounded-full border-2 border-t-transparent"></div>
			{:else}
				<SaveIcon class="size-5" />
			{/if}
		</Button>

		<!-- Status indicator for mobile -->
		{#if formState.hasChanges}
			<div class="absolute -top-2 -left-2 size-3 animate-pulse rounded-full bg-orange-500"></div>
		{/if}
	</div>
{/if}
