<script lang="ts">
	import { page } from '$app/state';
	import { goto, beforeNavigate } from '$app/navigation';
	import { setContext } from 'svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { SettingsIcon, ArrowRightIcon, ArrowLeftIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import settingsStore from '$lib/stores/config-store';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';
	import { cn } from '$lib/utils';
	import MobileFloatingFormActions from '$lib/components/form/mobile-floating-form-actions.svelte';

	interface Props {
		children: import('svelte').Snippet;
	}

	let { children }: Props = $props();

	let isSubPage = $derived(page.url.pathname !== '/settings');
	let currentPageName = $derived(page.url.pathname.split('/').pop() || 'settings');

	const isMobile = new IsMobile();
	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);
	let pageTitle = $derived.by(() => {
		switch (currentPageName) {
			case 'jobs':
				return m.jobs_title();
			case 'docker':
				return m.docker_title();
			case 'authentication':
				return m.authentication_title();
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
			case 'environments':
				return m.environments_title();
			case 'build':
				return 'Build';
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

<div class="flex h-full min-h-full flex-col">
	<!-- Main Content -->
	<main class="min-w-0 flex-1">
		{#if isSubPage}
			<div
				class={cn(
					'sticky top-4 z-5 mx-4 mb-6 rounded-lg border shadow-lg transition-all duration-200 md:hidden',
					'bg-background/95 backdrop-blur-md'
				)}
			>
				<div class="px-4 py-3">
					<div class="flex items-center justify-between gap-4">
						<div class="flex min-w-0 items-center gap-2">
							<ArcaneButton
								action="base"
								tone="ghost"
								onclick={goBackToSettings}
								class="text-muted-foreground hover:text-foreground shrink-0 gap-2"
								icon={ArrowLeftIcon}
								customLabel={m.common_back()}
								showLabel={!isMobile.current}
							/>

							<nav class="flex min-w-0 items-center gap-2 text-sm">
								<ArcaneButton
									action="base"
									tone="ghost"
									onclick={goBackToSettings}
									class="text-muted-foreground hover:text-foreground shrink-0 gap-2"
									icon={SettingsIcon}
									customLabel={m.settings_title()}
								/>
								<ArrowRightIcon class="text-muted-foreground size-4 shrink-0" />
								<span class="text-foreground truncate font-medium">{pageTitle}</span>
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
	<MobileFloatingFormActions
		hasChanges={formState.hasChanges}
		isLoading={formState.isLoading}
		onSave={handleSave}
		onReset={() => formState.resetFunction && formState.resetFunction()}
	/>
{/if}
