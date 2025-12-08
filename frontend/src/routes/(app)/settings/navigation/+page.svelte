<script lang="ts">
	import { z } from 'zod/v4';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';
	import EyeIcon from '@lucide/svelte/icons/eye';
	import NavigationIcon from '@lucide/svelte/icons/navigation';
	import NavigationSettingControl from '$lib/components/navigation-setting-control.svelte';
	import NavigationModeSettingControl from '$lib/components/navigation-mode-setting-control.svelte';
	import settingsStore from '$lib/stores/config-store';
	import { m } from '$lib/paraglide/messages';
	import { navigationSettingsOverridesStore, resetNavigationVisibility } from '$lib/utils/navigation.utils';
	import { SettingsPageLayout } from '$lib/layouts';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { useSidebar } from '$lib/components/ui/sidebar/context.svelte.js';
	import { createSettingsForm } from '$lib/utils/settings-form.util';
	import { Separator } from '$lib/components/ui/separator';
	import { Label } from '$lib/components/ui/label';

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
			getCurrentSettings: () => $settingsStore || data.settings!,
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
		<div class="space-y-8">
			<!-- Desktop Sidebar Section -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">{m.navigation_desktop_sidebar_title()}</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.navigation_sidebar_hover_expansion_label()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.navigation_sidebar_hover_expansion_description()}</p>
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
								<Label for="sidebarHoverExpansion" class="font-normal">
									{$formInputs.sidebarHoverExpansion.value
										? m.navigation_sidebar_hover_expansion_enabled()
										: m.navigation_sidebar_hover_expansion_disabled()}
								</Label>
							</div>
						</div>
					</div>
				</div>
			</div>

			<!-- Mobile Appearance Section -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">Mobile Appearance</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.navigation_mode_label()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.navigation_mode_description()}</p>
							</div>
							<div>
								<NavigationModeSettingControl
									id="mobileNavigationMode"
									label=""
									description=""
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
							</div>
						</div>

						<Separator />

						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.navigation_show_labels_label()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.navigation_show_labels_description()}</p>
							</div>
							<div>
								<NavigationSettingControl
									id="mobileNavigationShowLabels"
									label=""
									description=""
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
						</div>
					</div>
				</div>
			</div>
		</div>
	{/snippet}
</SettingsPageLayout>
