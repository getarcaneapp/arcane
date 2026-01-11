<script lang="ts">
	import { z } from 'zod/v4';
	import { toast } from 'svelte-sonner';
	import settingsStore from '$lib/stores/config-store';
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
	import { ApperanceIcon, MonitorSpeakerIcon, DockIcon } from '$lib/icons';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';

	let { data } = $props();
	const currentSettings = $derived($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore?.uiConfigDisabled);

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

	// Sidebar context is only available in desktop view
	let sidebar: ReturnType<typeof useSidebar> | null = null;
	try {
		sidebar = useSidebar();
	} catch {
		// Sidebar context not available (mobile view)
	}

	let { formInputs } = $derived(
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
	}

	// Navigation Mode state
	const modeIsLocal = $derived(persistedState.mode !== undefined);
	const modeDisplayValue = $derived(modeIsLocal ? persistedState.mode : $formInputs.mobileNavigationMode.value);

	function handleModeSelect(mode: 'floating' | 'docked') {
		if (modeIsLocal) {
			setLocalOverride('mode', mode);
		} else {
			$formInputs.mobileNavigationMode.value = mode;
		}
	}

	function handleModeScopeChange(isLocal: boolean) {
		if (isLocal) {
			setLocalOverride('mode', $formInputs.mobileNavigationMode.value);
		} else {
			clearLocalOverride('mode');
		}
	}

	// Show Labels state
	const labelsIsLocal = $derived(persistedState.showLabels !== undefined);
	const labelsDisplayValue = $derived(labelsIsLocal ? persistedState.showLabels : $formInputs.mobileNavigationShowLabels.value);

	function handleLabelsChange(checked: boolean) {
		if (labelsIsLocal) {
			setLocalOverride('showLabels', checked);
		} else {
			$formInputs.mobileNavigationShowLabels.value = checked;
		}
	}

	function handleLabelsScopeChange(isLocal: boolean) {
		if (isLocal) {
			setLocalOverride('showLabels', $formInputs.mobileNavigationShowLabels.value);
		} else {
			clearLocalOverride('showLabels');
		}
	}
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
						<!-- Navigation Mode -->
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.navigation_mode_label()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.navigation_mode_description()}</p>
							</div>
							<div class="space-y-3">
								<SwitchWithLabel
									id="mode-scope-toggle"
									checked={modeIsLocal}
									label={modeIsLocal ? m.this_device() : m.server_default()}
									onCheckedChange={handleModeScopeChange}
									disabled={isReadOnly && !modeIsLocal}
								/>

								<div class="grid grid-cols-2 gap-2">
									<ArcaneButton
										action="base"
										tone={modeDisplayValue === 'floating' ? 'outline-primary' : 'outline'}
										class="h-12 w-full"
										onclick={() => handleModeSelect('floating')}
										icon={MonitorSpeakerIcon}
										customLabel={m.navigation_mode_floating()}
									/>
									<ArcaneButton
										action="base"
										tone={modeDisplayValue === 'docked' ? 'outline-primary' : 'outline'}
										class="h-12 w-full"
										onclick={() => handleModeSelect('docked')}
										icon={DockIcon}
										customLabel={m.navigation_mode_docked()}
									/>
								</div>
							</div>
						</div>

						<Separator />

						<!-- Show Labels -->
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.navigation_show_labels_label()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.navigation_show_labels_description()}</p>
							</div>
							<div class="space-y-3">
								<SwitchWithLabel
									id="labels-scope-toggle"
									checked={labelsIsLocal}
									label={labelsIsLocal ? m.this_device() : m.server_default()}
									onCheckedChange={handleLabelsScopeChange}
									disabled={isReadOnly && !labelsIsLocal}
								/>

								<div class="flex items-center gap-3">
									<Switch id="mobileNavigationShowLabels" checked={labelsDisplayValue} onCheckedChange={handleLabelsChange} />
									<span class="text-sm font-medium">
										{labelsDisplayValue ? m.on() : m.off()}
									</span>
								</div>
							</div>
						</div>
					</div>
				</div>
			</div>
		</div>
	{/snippet}
</SettingsPageLayout>
