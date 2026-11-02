<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import type { ResolvedPathname } from '$app/types';
	import type { Snippet } from 'svelte';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import MobileFloatingFormActions from '#lib/components/form/mobile-floating-form-actions.svelte';
	import { getNavigationTitleForPath } from '#lib/config/navigation-config.js';
	import { IsMobile } from '#lib/hooks/is-mobile.svelte.js';
	import { setSettingsFormContext } from '#lib/hooks/settings-form-context.js';
	import { ArrowRightIcon, ArrowLeftIcon, type IconType } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import type { SettingsFormContext, SettingsFormState } from '#lib/types/settings-form.js';
	import { cn } from '#lib/utils.js';

	interface Props {
		rootUrl: ResolvedPathname;
		rootTitle: string;
		rootIcon: IconType;
		children: Snippet;
	}

	let { rootUrl, rootTitle, rootIcon, children }: Props = $props();

	const isSubPage = $derived(page.url.pathname !== rootUrl);
	const pageTitle = $derived(getNavigationTitleForPath(page.url.pathname) ?? rootTitle);
	const isMobile = new IsMobile();
	const isReadOnly = $derived(settingsStore.readOnly);

	let formState = $state.raw<SettingsFormState>();
	let registeredForm: SettingsFormState | undefined;
	const formContext: SettingsFormContext = {
		get activeForm() {
			return formState;
		},
		register(form) {
			registeredForm = form;
			formState = form;
		},
		release(form) {
			if (registeredForm !== form) return;
			registeredForm = undefined;
			formState = undefined;
		}
	};
	setSettingsFormContext(formContext);

	function goBackToRoot() {
		goto(rootUrl);
	}

	async function handleSave() {
		if (formState?.saveFunction) {
			await formState.saveFunction();
		}
	}
</script>

<div class="flex h-full min-h-full flex-col">
	<div class="min-w-0 flex-1">
		{#if isSubPage}
			<div
				class={cn(
					'sticky top-4 z-(--arcane-z-sticky) mx-4 mb-6 rounded-lg border shadow-lg desktop-layout:hidden',
					'bg-background/95 backdrop-blur-md'
				)}
			>
				<div class="px-4 py-3">
					<div class="flex items-center justify-between gap-4">
						<div class="flex min-w-0 items-center gap-2">
							<ArcaneButton
								action="base"
								tone="ghost"
								onclick={goBackToRoot}
								class="shrink-0 gap-2 text-muted-foreground hover:text-foreground"
								icon={ArrowLeftIcon}
								customLabel={m.common_back()}
								showLabel={!isMobile.current}
							/>

							<nav class="flex min-w-0 items-center gap-2 text-sm">
								<ArcaneButton
									action="base"
									tone="ghost"
									onclick={goBackToRoot}
									class="shrink-0 gap-2 text-muted-foreground hover:text-foreground"
									icon={rootIcon}
									customLabel={rootTitle}
								/>
								<ArrowRightIcon class="size-4 shrink-0 text-muted-foreground" />
								<span class="truncate font-medium text-foreground">{pageTitle}</span>
							</nav>
						</div>
					</div>
				</div>
			</div>
		{/if}

		<div>
			<div class="w-full max-w-none">
				{@render children()}
			</div>
		</div>
	</div>
</div>

{#if isSubPage && !isReadOnly && formState?.saveFunction}
	<MobileFloatingFormActions
		hasChanges={formState.hasChanges}
		isLoading={formState.isLoading}
		onSave={handleSave}
		onReset={() => formState?.resetFunction?.()}
	/>
{/if}
