<script lang="ts">
	import { page } from '$app/state';
	import { goto, beforeNavigate } from '$app/navigation';
	import { setContext } from 'svelte';
	import { Button } from '$lib/components/ui/button';
	import { SettingsIcon, ArrowRightIcon, ArrowLeftIcon, SaveIcon, ResetIcon } from '$lib/icons';
	import { useSidebar } from '$lib/components/ui/sidebar/context.svelte.js';
	import { m } from '$lib/paraglide/messages';
	import settingsStore from '$lib/stores/config-store';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';
	import { IsTablet } from '$lib/hooks/is-tablet.svelte.js';
	import { getEffectiveNavigationSettings } from '$lib/utils/navigation.utils';
	import { cn } from '$lib/utils';
	import { navigationItems } from '$lib/config/navigation-config';

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
	const isGlassEnabled = $derived.by(() => $settingsStore?.glassEffectEnabled ?? false);
	const navigationSettings = $derived(getEffectiveNavigationSettings());
	const navigationMode = $derived(navigationSettings.mode);
	const scrollToHideEnabled = $derived(navigationSettings.scrollToHide);

	const navItems = $derived.by(() => {
		const settingsEntry = navigationItems.settingsItems.find((item) => item.url === '/settings');
		return (
			settingsEntry?.items?.map((item) => ({
				href: item.url,
				label: item.title,
				icon: item.icon
			})) ?? []
		);
	});

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
			case 'notifications':
				return m.notifications_title();
			case 'api-keys':
				return m.api_key_page_title();
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

	// Reset form state before navigating to a new page
	beforeNavigate(() => {
		formState.hasChanges = false;
		formState.isLoading = false;
		formState.saveFunction = null;
		formState.resetFunction = null;
	});

	function goBackToSettings() {
		goto('/settings');
	}

	async function handleSave() {
		if (formState.saveFunction) {
			await formState.saveFunction();
		}
	}
</script>

<div class="flex min-h-[calc(100vh-4rem)] flex-col md:flex-row">
	<!-- Desktop Sidebar -->
	<aside
		class={cn(
			'hidden w-64 shrink-0 border-r md:block',
			isGlassEnabled ? 'bg-background/50 backdrop-blur-sm' : 'bg-card'
		)}
	>
		<div class="sticky top-0 px-3 py-4">
			<h2 class="mb-4 px-4 text-lg font-semibold tracking-tight">{m.settings_title()}</h2>
			<nav class="space-y-1">
				{#each navItems as item (item.href)}
					{@const isActive = currentPath.startsWith(item.href)}
					<a
						href={item.href}
						class={cn(
							'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
							isActive ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-muted hover:text-foreground'
						)}
					>
						<item.icon class="size-4" />
						{item.label}
					</a>
				{/each}
			</nav>
		</div>
	</aside>

	<!-- Main Content -->
	<main class="min-w-0 flex-1">
		{#if isSubPage}
			<div
				class={cn(
					'sticky top-4 z-5 mx-4 mb-6 rounded-lg border shadow-lg transition-all duration-200 md:hidden',
					isGlassEnabled ? 'bg-background/95 backdrop-blur-md' : 'bg-card'
				)}
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
								<span class="hidden sm:inline">{m.common_back()}</span>
							</Button>

							<nav class="flex min-w-0 items-center gap-2 text-sm">
								<Button
									variant="ghost"
									size="sm"
									onclick={goBackToSettings}
									class="text-muted-foreground hover:text-foreground shrink-0 gap-2"
								>
									<SettingsIcon class="size-4" />
									<span>{m.settings_title()}</span>
								</Button>
								<ArrowRightIcon class="text-muted-foreground size-4 shrink-0" />
								<span class="text-foreground truncate font-medium">{pageTitle()}</span>
							</nav>
						</div>
					</div>
				</div>
			</div>
		{/if}

		<div class="settings-container">
			<div class="settings-content w-full max-w-none">
				{@render children()}
			</div>
		</div>
	</main>
</div>

<!-- Mobile Floating Action Buttons -->
{#if isSubPage && !isReadOnly && formState.saveFunction}
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
				class={cn(
					'size-14 rounded-full border-2 shadow-lg',
					isGlassEnabled ? 'bg-background/80 backdrop-blur-md' : 'bg-card'
				)}
			>
				<ResetIcon class="size-5" />
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
