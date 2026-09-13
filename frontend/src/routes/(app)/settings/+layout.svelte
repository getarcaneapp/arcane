<script lang="ts">
	import type { LayoutProps } from './$types';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { setSettingsFormContext } from '#lib/hooks/settings-form-context.js';
	import type { SettingsFormContext, SettingsFormState } from '#lib/types/settings-form.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { SettingsIcon, ArrowRightIcon, ArrowLeftIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { IsMobile } from '#lib/hooks/is-mobile.svelte.js';
	import { cn } from '#lib/utils.js';
	import MobileFloatingFormActions from '#lib/components/form/mobile-floating-form-actions.svelte';

	let { children }: LayoutProps = $props();

	let isSubPage = $derived(page.url.pathname !== '/settings');
	let currentPageName = $derived(page.url.pathname.split('/').pop() || 'settings');

	const isMobile = new IsMobile();
	const isReadOnly = $derived.by(() => settingsStore.current?.uiConfigDisabled);
	let pageTitle = $derived.by(() => {
		switch (currentPageName) {
			case 'jobs':
				return m.automations();
			case 'docker':
				return m.docker_title();
			case 'authentication':
				return m.authentication();
			case 'security':
				return m.security();
			case 'users':
				return m.users_title();
			case 'notifications':
				return m.notifications_title();
			case 'api-keys':
				return m.api_key_page_title();
			case 'webhooks':
				return m.webhook_page_title();
			case 'build':
				return m.build();
			case 'diagnostics':
				return m.diagnostics();
			default:
				return m.settings();
		}
	});

	let formState = $state.raw<SettingsFormState>();
	const formContext: SettingsFormContext = {
		get activeForm() {
			return formState;
		},
		set activeForm(form) {
			formState = form;
		}
	};
	setSettingsFormContext(formContext);

	function goBackToSettings() {
		goto('/settings');
	}

	async function handleSave() {
		if (formState?.saveFunction) {
			await formState.saveFunction();
		}
	}
</script>

<div class="flex h-full min-h-full flex-col">
	<main class="min-w-0 flex-1">
		{#if isSubPage}
			<div
				class={cn(
					'sticky top-4 z-[var(--arcane-z-sticky)] mx-4 mb-6 rounded-lg border shadow-lg md:hidden',
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
								class="shrink-0 gap-2 text-muted-foreground hover:text-foreground"
								icon={ArrowLeftIcon}
								customLabel={m.common_back()}
								showLabel={!isMobile.current}
							/>

							<nav class="flex min-w-0 items-center gap-2 text-sm">
								<ArcaneButton
									action="base"
									tone="ghost"
									onclick={goBackToSettings}
									class="shrink-0 gap-2 text-muted-foreground hover:text-foreground"
									icon={SettingsIcon}
									customLabel={m.settings()}
								/>
								<ArrowRightIcon class="size-4 shrink-0 text-muted-foreground" />
								<span class="truncate font-medium text-foreground">{pageTitle}</span>
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

{#if isSubPage && !isReadOnly && formState?.saveFunction}
	<MobileFloatingFormActions
		hasChanges={formState.hasChanges}
		isLoading={formState.isLoading}
		onSave={handleSave}
		onReset={() => formState?.resetFunction?.()}
	/>
{/if}
