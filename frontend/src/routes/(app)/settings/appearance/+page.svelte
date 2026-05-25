<script lang="ts">
	import { onDestroy } from 'svelte';
	import { z } from 'zod/v4';
	import { mode, toggleMode } from 'mode-watcher';
	import settingsStore from '$lib/stores/config-store';
	import { m } from '$lib/paraglide/messages';
	import { navigationSettingsOverridesStore, resetNavigationVisibility } from '$lib/utils/navigation';
	import { SettingsPageLayout } from '$lib/layouts';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { useSidebar } from '$lib/components/ui/sidebar/context.svelte.js';
	import { createSettingsForm } from '$lib/utils/settings';
	import SettingsRow from '$lib/components/settings/settings-row.svelte';
	import LocalePicker from '$lib/components/locale-picker.svelte';
	import AccentColorPicker from '$lib/components/accent-color/accent-color-picker.svelte';
	import ApplicationThemePicker from '$lib/components/application-theme/application-theme-picker.svelte';
	import { applyAccentColor } from '$lib/utils/theme';
	import { APPLICATION_THEME_VALUES, applyApplicationTheme } from '$lib/utils/theme';
	import { applyOledMode } from '$lib/utils/theme';
	import { AppearanceIcon, MonitorSpeakerIcon, DockIcon, MoonIcon, SunIcon } from '$lib/icons';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { cn } from '$lib/utils';

	let { data } = $props();
	const currentSettings = $derived($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore?.uiConfigDisabled);

	const formSchema = z.object({
		applicationTheme: z.enum(APPLICATION_THEME_VALUES),
		mobileNavigationMode: z.enum(['floating', 'docked']),
		mobileNavigationShowLabels: z.boolean(),
		sidebarHoverExpansion: z.boolean(),
		keyboardShortcutsEnabled: z.boolean(),
		accentColor: z.string(),
		enableGravatar: z.boolean(),
		oledMode: z.boolean()
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
			onReset: restorePersistedAppearance
		})
	);

	function restorePersistedAppearance() {
		applyApplicationTheme(currentSettings.applicationTheme);
		applyAccentColor(currentSettings.accentColor);
		applyOledMode(currentSettings.oledMode ?? false);
	}

	onDestroy(() => {
		restorePersistedAppearance();
	});

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
	const modeSegment = $derived<'default' | 'floating' | 'docked'>(
		modeIsLocal ? (persistedState.mode as 'floating' | 'docked') : 'default'
	);

	function handleModeSegmentSelect(segment: 'default' | 'floating' | 'docked') {
		if (segment === 'default') {
			clearLocalOverride('mode');
		} else {
			setLocalOverride('mode', segment);
		}
	}

	// Show Labels state
	const labelsIsLocal = $derived(persistedState.showLabels !== undefined);
	const labelsSegment = $derived<'default' | 'on' | 'off'>(
		labelsIsLocal ? (persistedState.showLabels ? 'on' : 'off') : 'default'
	);
	const isDarkMode = $derived(mode.current === 'dark');
	const isDefaultApplicationTheme = $derived($formInputs.applicationTheme.value === 'default');

	function handleOledModeChange(checked: boolean) {
		$formInputs.oledMode.value = checked;
		// Live preview: apply immediately so the user sees the effect
		applyOledMode(checked);
	}

	function handleLabelsSegmentSelect(segment: 'default' | 'on' | 'off') {
		if (segment === 'default') {
			clearLocalOverride('showLabels');
		} else {
			setLocalOverride('showLabels', segment === 'on');
		}
	}
</script>

<SettingsPageLayout
	title={m.appearance_title()}
	description={m.appearance_description()}
	icon={AppearanceIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<div class="space-y-8">
			<!-- Appearance Section -->
			<div class="space-y-4">
				<h3 class="text-base font-semibold">{m.appearance_title()}</h3>
				<div class="divide-border/40 divide-y [&>*]:py-5 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0">
					<!-- Application Theme -->
					<SettingsRow label={m.application_theme()} description={m.application_theme_description()}>
						<ApplicationThemePicker
							bind:selectedTheme={$formInputs.applicationTheme.value}
							accentColor={$formInputs.accentColor.value}
							disabled={isReadOnly}
						/>
					</SettingsRow>

					<!-- Accent Color -->
					<SettingsRow label={m.accent_color()} description={m.accent_color_description()}>
						<AccentColorPicker
							previousColor={currentSettings.accentColor}
							bind:selectedColor={$formInputs.accentColor.value}
							disabled={isReadOnly}
						/>
					</SettingsRow>

					<!-- User Avatars -->
					<SettingsRow
						label={m.general_user_avatars_heading()}
						description={m.general_user_avatars_description()}
						layout="inline"
					>
						<Switch
							id="enableGravatar"
							bind:checked={$formInputs.enableGravatar.value}
							disabled={isReadOnly}
							onCheckedChange={(checked) => {
								$formInputs.enableGravatar.value = checked;
							}}
						/>
					</SettingsRow>

					<!-- Language -->
					<SettingsRow label={m.language()} description={m.appearance_language_current_user_description()} layout="inline">
						<LocalePicker
							inline={true}
							id="appearanceLocalePicker"
							class="border-border/30 text-foreground h-9 w-32 text-sm font-medium"
						/>
					</SettingsRow>

					<!-- Theme -->
					<SettingsRow
						label={m.common_toggle_theme()}
						description={m.appearance_theme_current_user_description()}
						layout="inline"
					>
						<ArcaneButton action="base" tone="outline" class="h-9 min-w-40 justify-start gap-2" onclick={toggleMode}>
							{#if isDarkMode}
								<SunIcon class="size-4" />
							{:else}
								<MoonIcon class="size-4" />
							{/if}
							<span>{isDarkMode ? m.sidebar_dark_mode() : m.sidebar_light_mode()}</span>
						</ArcaneButton>
					</SettingsRow>

					<!-- OLED Mode -->
					<SettingsRow label={m.oled_mode()} description={m.oled_mode_description()} layout="inline">
						{#snippet helpText()}
							{#if !isDefaultApplicationTheme}
								<p class="text-muted-foreground/70 mt-1 text-xs italic">{m.oled_mode_requires_default_theme()}</p>
							{:else if !isDarkMode}
								<p class="text-muted-foreground/70 mt-1 text-xs italic">{m.oled_mode_requires_dark()}</p>
							{/if}
						{/snippet}
						<Switch
							id="oledMode"
							checked={$formInputs.oledMode.value}
							disabled={isReadOnly || !isDefaultApplicationTheme}
							onCheckedChange={handleOledModeChange}
						/>
					</SettingsRow>
				</div>
			</div>

			<!-- Desktop Sidebar Section -->
			<div class="space-y-4">
				<h3 class="text-base font-semibold">{m.navigation_desktop_sidebar_title()}</h3>
				<div class="divide-border/40 divide-y [&>*]:py-5 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0">
					<SettingsRow
						label={m.navigation_sidebar_hover_expansion_label()}
						description={m.navigation_sidebar_hover_expansion_description()}
						layout="inline"
					>
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
					</SettingsRow>

					<!-- Keyboard Shortcuts -->
					<SettingsRow
						label={m.navigation_keyboard_shortcuts_label()}
						description={m.navigation_keyboard_shortcuts_description()}
						layout="inline"
					>
						<Switch
							id="keyboardShortcutsEnabled"
							checked={$formInputs.keyboardShortcutsEnabled.value}
							disabled={isReadOnly}
							onCheckedChange={(checked) => {
								$formInputs.keyboardShortcutsEnabled.value = checked;
							}}
						/>
					</SettingsRow>
				</div>
			</div>

			<!-- Mobile Appearance Section -->
			<div class="space-y-4">
				<h3 class="text-base font-semibold">{m.navigation_mobile_appearance_title()}</h3>
				<div class="divide-border/40 divide-y [&>*]:py-5 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0">
					<!-- Navigation Mode -->
					<SettingsRow label={m.navigation_mode_label()} description={m.navigation_mode_description()}>
						<div class="bg-muted/40 inline-flex rounded-lg p-0.5">
							<button
								type="button"
								disabled={isReadOnly && modeSegment !== 'default'}
								onclick={() => handleModeSegmentSelect('default')}
								class={cn(
									'rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
									modeSegment === 'default'
										? 'bg-background text-foreground shadow-sm'
										: 'text-muted-foreground hover:text-foreground'
								)}
							>
								{m.server_default()}
							</button>
							<button
								type="button"
								disabled={isReadOnly && modeSegment === 'default'}
								onclick={() => handleModeSegmentSelect('floating')}
								class={cn(
									'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
									modeSegment === 'floating'
										? 'bg-background text-foreground shadow-sm'
										: 'text-muted-foreground hover:text-foreground'
								)}
							>
								<MonitorSpeakerIcon class="size-3.5" />
								{m.navigation_mode_floating()}
							</button>
							<button
								type="button"
								disabled={isReadOnly && modeSegment === 'default'}
								onclick={() => handleModeSegmentSelect('docked')}
								class={cn(
									'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
									modeSegment === 'docked'
										? 'bg-background text-foreground shadow-sm'
										: 'text-muted-foreground hover:text-foreground'
								)}
							>
								<DockIcon class="size-3.5" />
								{m.navigation_mode_docked()}
							</button>
						</div>
					</SettingsRow>

					<!-- Show Labels -->
					<SettingsRow label={m.navigation_show_labels_label()} description={m.navigation_show_labels_description()}>
						<div class="bg-muted/40 inline-flex rounded-lg p-0.5">
							<button
								type="button"
								disabled={isReadOnly && labelsSegment !== 'default'}
								onclick={() => handleLabelsSegmentSelect('default')}
								class={cn(
									'rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
									labelsSegment === 'default'
										? 'bg-background text-foreground shadow-sm'
										: 'text-muted-foreground hover:text-foreground'
								)}
							>
								{m.server_default()}
							</button>
							<button
								type="button"
								disabled={isReadOnly && labelsSegment === 'default'}
								onclick={() => handleLabelsSegmentSelect('on')}
								class={cn(
									'rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
									labelsSegment === 'on'
										? 'bg-background text-foreground shadow-sm'
										: 'text-muted-foreground hover:text-foreground'
								)}
							>
								{m.on()}
							</button>
							<button
								type="button"
								disabled={isReadOnly && labelsSegment === 'default'}
								onclick={() => handleLabelsSegmentSelect('off')}
								class={cn(
									'rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
									labelsSegment === 'off'
										? 'bg-background text-foreground shadow-sm'
										: 'text-muted-foreground hover:text-foreground'
								)}
							>
								{m.off()}
							</button>
						</div>
					</SettingsRow>
				</div>
			</div>
		</div>
	{/snippet}
</SettingsPageLayout>
