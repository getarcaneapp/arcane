<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { z } from 'zod/v4';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';
	import EyeIcon from '@lucide/svelte/icons/eye';
	import NavigationIcon from '@lucide/svelte/icons/navigation';
	import SidebarIcon from '@lucide/svelte/icons/sidebar';
	import NavigationSettingControl from '$lib/components/navigation-setting-control.svelte';
	import NavigationModeSettingControl from '$lib/components/navigation-mode-setting-control.svelte';
	import settingsStore from '$lib/stores/config-store';
	import { m } from '$lib/paraglide/messages';
	import { navigationSettingsOverridesStore, resetNavigationVisibility } from '$lib/utils/navigation.utils';
	import { SettingsPageLayout } from '$lib/layouts';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { useSidebar } from '$lib/components/ui/sidebar/context.svelte.js';
	import { createSettingsForm } from '$lib/utils/settings-form.util';

	let { data } = $props();
	const currentSettings = $derived($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore?.uiConfigDisabled);

	const formSchema = z.object({
		mobileNavigationMode: z.enum(['floating', 'docked']),
		mobileNavigationShowLabels: z.boolean(),
		sidebarHoverExpansion: z.boolean()
	});

	// Track local override state using the shared store
	let persistedState = $state(navigationSettingsOverridesStore.current);

	// Sidebar context is only available in desktop view
	let sidebar: ReturnType<typeof useSidebar> | null = null;
	try {
		sidebar = useSidebar();
	} catch {
		// Sidebar context not available (mobile view)
	}

	let { formInputs, registerOnMount } = $derived(
		createSettingsForm({
			schema: formSchema,
			currentSettings,
			successMessage: m.navigation_settings_saved()
		})
	);

	function setLocalOverride(key: 'mode' | 'showLabels', value: any) {
		const currentOverrides = navigationSettingsOverridesStore.current;
		navigationSettingsOverridesStore.current = { ...currentOverrides, [key]: value };
		persistedState = navigationSettingsOverridesStore.current;
		if (key === 'mode') resetNavigationVisibility();
	}

	function clearLocalOverride(key: 'mode' | 'showLabels') {
		const currentOverrides = navigationSettingsOverridesStore.current;
		const newOverrides = { ...currentOverrides };
		delete newOverrides[key];
		navigationSettingsOverridesStore.current = newOverrides;
		persistedState = navigationSettingsOverridesStore.current;
		if (key === 'mode') resetNavigationVisibility();
		toast.success(`Local override cleared for ${key.replace(/([A-Z])/g, ' $1').toLowerCase()}`);
	}

	onMount(() => registerOnMount());
</script>

<SettingsPageLayout
	title={m.navigation_title()}
	description={m.navigation_description()}
	icon={NavigationIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<div class="space-y-4 sm:space-y-6">
			<Card.Root>
				<Card.Header icon={SidebarIcon}>
					<div class="flex flex-col space-y-1.5">
						<Card.Title>{m.navigation_desktop_sidebar_title()}</Card.Title>
						<Card.Description>{m.navigation_desktop_sidebar_description()}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="px-3 py-3 sm:px-6 sm:py-4">
					<div class="flex items-start gap-3 rounded-lg border p-3 sm:p-4">
						<div
							class="bg-primary/10 text-primary ring-primary/20 flex size-7 shrink-0 items-center justify-center rounded-lg ring-1 sm:size-8"
						>
							<SidebarIcon class="size-3 sm:size-4" />
						</div>
						<div class="flex flex-1 flex-col gap-3">
							<div>
								<h4 class="mb-1 text-sm leading-tight font-medium">{m.navigation_sidebar_hover_expansion_label()}</h4>
								<p class="text-muted-foreground text-xs leading-relaxed">
									{m.navigation_sidebar_hover_expansion_description()}
								</p>
							</div>
							<div class="flex items-center gap-2">
								<Switch
									id="sidebarHoverExpansion"
									checked={$formInputs.sidebarHoverExpansion.value}
									disabled={isReadOnly}
									onCheckedChange={(checked) => {
										$formInputs.sidebarHoverExpansion.value = checked;
										// Update the sidebar immediately if context is available
										if (sidebar) {
											sidebar.setHoverExpansion(checked);
										}
									}}
								/>
								<label for="sidebarHoverExpansion" class="text-xs font-medium">
									{$formInputs.sidebarHoverExpansion.value
										? m.navigation_sidebar_hover_expansion_enabled()
										: m.navigation_sidebar_hover_expansion_disabled()}
								</label>
							</div>
						</div>
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header icon={NavigationIcon}>
					<div class="flex flex-col space-y-1.5">
						<Card.Title>{m.navigation_mobile_appearance_title()}</Card.Title>
						<Card.Description>{m.navigation_mobile_appearance_description()}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="px-3 py-3 sm:px-6 sm:py-4">
					<div class="grid grid-cols-1 gap-3 sm:gap-4 lg:grid-cols-[repeat(auto-fit,minmax(400px,1fr))]">
						<NavigationModeSettingControl
							id="mobileNavigationMode"
							label={m.navigation_mode_label()}
							description={m.navigation_mode_description()}
							icon={NavigationIcon}
							serverValue={$formInputs.mobileNavigationMode.value}
							localOverride={persistedState.mode}
							onServerChange={(value) => {
								$formInputs.mobileNavigationMode.value = value;
							}}
							onLocalOverride={(value) => setLocalOverride('mode', value)}
							onClearOverride={() => clearLocalOverride('mode')}
							serverDisabled={isReadOnly}
						/>

						<NavigationSettingControl
							id="mobileNavigationShowLabels"
							label={m.navigation_show_labels_label()}
							description={m.navigation_show_labels_description()}
							icon={EyeIcon}
							serverValue={$formInputs.mobileNavigationShowLabels.value}
							localOverride={persistedState.showLabels}
							onServerChange={(value) => {
								$formInputs.mobileNavigationShowLabels.value = value;
							}}
							onLocalOverride={(value) => setLocalOverride('showLabels', value)}
							onClearOverride={() => clearLocalOverride('showLabels')}
							serverDisabled={isReadOnly}
						/>
					</div>
				</Card.Content>
			</Card.Root>
		</div>
	{/snippet}
</SettingsPageLayout>
