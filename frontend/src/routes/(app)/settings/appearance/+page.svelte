<script lang="ts">
	import { z } from 'zod/v4';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';
	import NavigationSettingControl from '$lib/components/navigation-setting-control.svelte';
	import NavigationModeSettingControl from '$lib/components/navigation-mode-setting-control.svelte';
	import MobileDockTabSelector from '$lib/components/mobile-dock-tab-selector.svelte';
	import settingsStore from '$lib/stores/config-store';
	import userStore from '$lib/stores/user-store';
	import { m } from '$lib/paraglide/messages';
	import { navigationSettingsOverridesStore, resetNavigationVisibility } from '$lib/utils/navigation.utils';
	import { SettingsPageLayout } from '$lib/layouts';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { useSidebar } from '$lib/components/ui/sidebar/context.svelte.js';
	import { createSettingsForm } from '$lib/utils/settings-form.util';
	import { Separator } from '$lib/components/ui/separator';
	import { Label } from '$lib/components/ui/label';
	import AccentColorPicker from '$lib/components/accent-color/accent-color-picker.svelte';
	import { applyAccentColor } from '$lib/utils/accent-color-util';
	import { ApperanceIcon } from '$lib/icons';
	import { userService } from '$lib/services/user-service';
	import { defaultMobilePinnedItems } from '$lib/config/navigation-config';

	let { data } = $props();
	const currentSettings = $derived($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore?.uiConfigDisabled);
	const currentUser = $derived(data.user);

	const formSchema = z.object({
		mobileNavigationMode: z.enum(['floating', 'docked']),
		mobileNavigationShowLabels: z.boolean(),
		sidebarHoverExpansion: z.boolean(),
		accentColor: z.string(),
		glassEffectEnabled: z.boolean(),
		enableGravatar: z.boolean()
	});

	// Track local override state using the shared store
	let persistedState = $state(navigationSettingsOverridesStore.current);

	// Track mobile dock tabs
	const getInitialDockTabs = () => currentUser?.mobileDockTabs || defaultMobilePinnedItems.map((item) => item.url);
	let mobileDockTabs = $state<string[]>(getInitialDockTabs());
	let initialMobileDockTabs = $state<string[]>(getInitialDockTabs());
	const mobileDockTabsChanged = $derived(JSON.stringify(mobileDockTabs) !== JSON.stringify(initialMobileDockTabs));

	async function saveMobileDockTabs() {
		if (!currentUser || mobileDockTabs.length !== 4) {
			toast.error(m.mobile_dock_tabs_required());
			return;
		}

		try {
			await userService.update(currentUser.id, { mobileDockTabs });
			await userStore.reload();
			initialMobileDockTabs = [...mobileDockTabs];
			toast.success('Mobile dock tabs saved successfully');
		} catch (error) {
			toast.error('Failed to save mobile dock tabs');
			console.error('Error saving mobile dock tabs:', error);
		}
	}

	function resetMobileDockTabs() {
		mobileDockTabs = [...initialMobileDockTabs];
	}

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
			successMessage: m.navigation_settings_saved(),
			onReset: () => applyAccentColor(currentSettings.accentColor)
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
	title={m.appearance_title()}
	description={m.appearance_description()}
	icon={ApperanceIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<div class="space-y-8">
			<!-- Appearance Section -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">{m.appearance_title()}</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<!-- Accent Color -->
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.accent_color()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.accent_color_description()}</p>
							</div>
							<div>
								<AccentColorPicker
									previousColor={currentSettings.accentColor}
									bind:selectedColor={$formInputs.accentColor.value}
									disabled={isReadOnly}
								/>
							</div>
						</div>

						<Separator />

						<!-- Glass Effect -->
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.glass_effect_title()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.glass_effect_description()}</p>
							</div>
							<div class="flex items-center gap-2">
								<Switch
									id="glassEffectEnabled"
									bind:checked={$formInputs.glassEffectEnabled.value}
									disabled={isReadOnly}
									onCheckedChange={(checked) => {
										$formInputs.glassEffectEnabled.value = checked;
									}}
								/>
								<Label for="glassEffectEnabled" class="font-normal">
									{$formInputs.glassEffectEnabled.value ? m.glass_effect_enabled() : m.glass_effect_disabled()}
								</Label>
							</div>
						</div>

						<Separator />

						<!-- User Avatars -->
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.general_user_avatars_heading()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.general_user_avatars_description()}</p>
							</div>
							<div class="flex items-center gap-2">
								<Switch
									id="enableGravatar"
									bind:checked={$formInputs.enableGravatar.value}
									disabled={isReadOnly}
									onCheckedChange={(checked) => {
										$formInputs.enableGravatar.value = checked;
									}}
								/>
								<Label for="enableGravatar" class="font-normal">
									{m.general_enable_gravatar_label()}
								</Label>
							</div>
						</div>
					</div>
				</div>
			</div>

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
				<h3 class="text-lg font-medium">{m.navigation_mobile_appearance_title()}</h3>
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
									description=""
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
									description=""
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

						<Separator />

						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.mobile_dock_customization_title()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.mobile_dock_customization_description()}</p>
							</div>
							<div class="space-y-3">
								<MobileDockTabSelector bind:selectedTabs={mobileDockTabs} disabled={isReadOnly} />
								{#if mobileDockTabsChanged}
									<div class="flex gap-2">
										<button
											type="button"
											class="bg-primary text-primary-foreground hover:bg-primary/90 rounded-md px-3 py-1.5 text-sm font-medium"
											onclick={saveMobileDockTabs}
											disabled={mobileDockTabs.length !== 4 || isReadOnly}
										>
											{m.common_save()}
										</button>
										<button
											type="button"
											class="border-input bg-background hover:bg-accent hover:text-accent-foreground rounded-md border px-3 py-1.5 text-sm font-medium"
											onclick={resetMobileDockTabs}
											disabled={isReadOnly}
										>
											{m.common_reset()}
										</button>
									</div>
								{/if}
							</div>
						</div>
					</div>
				</div>
			</div>
		</div>
	{/snippet}
</SettingsPageLayout>
