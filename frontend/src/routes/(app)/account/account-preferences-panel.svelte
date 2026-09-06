<script lang="ts">
	import { mode } from 'mode-watcher';
	import ThemeModeSelector from '#lib/components/theme-mode/theme-mode-selector.svelte';
	import LocalePicker from '#lib/components/locale-picker.svelte';
	import TimeFormatPicker from '#lib/components/time-format-picker.svelte';
	import FontSizePicker from '#lib/components/font-size-picker.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import AccentColorPicker from '#lib/components/accent-color/accent-color-picker.svelte';
	import ApplicationThemePicker from '#lib/components/application-theme/application-theme-picker.svelte';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { DEFAULT_LANDING_PAGE, getLandingPageNavItems } from '#lib/config/navigation-config.js';
	import { resetNavigationVisibility } from '#lib/utils/navigation.js';
	import { applyGlassEffects, applyInterfaceAnimations, applyOledMode, DEFAULT_ACCENT_COLOR } from '#lib/utils/theme.js';
	import { debounced } from '#lib/utils/ws.js';
	import { m } from '#lib/paraglide/messages.js';
	import { userService } from '#lib/services/user-service.js';
	import userStore from '#lib/stores/user-store.js';
	import type { UserPreferences } from '#lib/types/auth.js';
	import type { ApplicationTheme, IconCatalog } from '#lib/types/settings.js';
	import { DockIcon, MonitorSpeakerIcon } from '#lib/icons/index.js';
	import { cn } from '#lib/utils.js';
	import { toast } from 'svelte-sonner';

	const currentUser = $derived($userStore);
	const preferences = $derived(currentUser?.preferences ?? {});
	const applicationThemeValue = $derived<ApplicationTheme>(preferences.applicationTheme ?? 'default');
	const accentColorValue = $derived(
		preferences.accentColor && preferences.accentColor !== 'default' ? preferences.accentColor : DEFAULT_ACCENT_COLOR
	);
	const iconCatalogValue = $derived(preferences.iconCatalog ?? 'selfhst');
	const oledModeEnabled = $derived(preferences.oledMode ?? false);
	const glassEffectsEnabled = $derived(preferences.glassEffectsEnabled ?? true);
	const animationsEnabled = $derived(preferences.animationsEnabled ?? true);
	const sidebarHoverExpansionEnabled = $derived(preferences.sidebarHoverExpansion ?? true);
	const keyboardShortcutsEnabled = $derived(preferences.keyboardShortcutsEnabled ?? true);
	const mobileNavigationMode = $derived(preferences.mobileNavigationMode ?? 'floating');
	const mobileNavigationShowLabels = $derived(preferences.mobileNavigationShowLabels ?? true);
	const isDarkMode = $derived(mode.current === 'dark');
	const isDefaultApplicationTheme = $derived(applicationThemeValue === 'default');

	const iconCatalogOptions = $derived([
		{ value: 'selfhst', label: m.icon_catalog_selfhst(), description: m.icon_catalog_selfhst_description() },
		{
			value: 'dashboard-icons',
			label: m.icon_catalog_dashboard_icons(),
			description: m.icon_catalog_dashboard_icons_description()
		}
	]);
	const landingPageOptions = $derived(getLandingPageNavItems().map((item) => ({ value: item.url, label: item.title })));
	const landingValue = $derived.by(() => {
		const current = preferences.defaultLandingPage ?? DEFAULT_LANDING_PAGE;
		return landingPageOptions.some((option) => option.value === current) ? current : DEFAULT_LANDING_PAGE;
	});

	async function savePreferences(patch: Partial<UserPreferences>) {
		const previous = currentUser;
		if (!previous) return;
		try {
			const updated = await userService.updateMyProfile({ preferences: patch });
			await userStore.setUser(updated);
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.common_update_failed({ resource: m.account_preferences() }));
			await userStore.setUser(previous);
		}
	}

	let pendingPreferences: Partial<UserPreferences> = {};
	const flushPreferences = debounced(() => {
		const patch = pendingPreferences;
		pendingPreferences = {};
		void savePreferences(patch);
	}, 400);

	function savePreferencesDebounced(patch: Partial<UserPreferences>) {
		pendingPreferences = { ...pendingPreferences, ...patch };
		flushPreferences();
	}

	function selectMobileNavigationMode(value: 'floating' | 'docked') {
		if (value === mobileNavigationMode) return;
		resetNavigationVisibility();
		void savePreferences({ mobileNavigationMode: value });
	}
</script>

<div class="space-y-10">
	<section>
		<h3 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.general_title()}</h3>
		<div class="mt-4 divide-y divide-border/40 [&>*]:py-4 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0">
			<SettingsRow label={m.language()} description={m.account_language_desc()} layout="inline">
				<LocalePicker inline id="accountLocalePicker" />
			</SettingsRow>
			<SettingsRow label={m.time_format()} description={m.account_time_format_desc()} layout="inline">
				<TimeFormatPicker id="accountTimeFormatPicker" />
			</SettingsRow>
		</div>
	</section>

	<section>
		<h3 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.appearance_title()}</h3>
		<div class="mt-4 divide-y divide-border/40 [&>*]:py-4 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0">
			<SettingsRow label={m.account_theme()} description={m.appearance_theme_current_user_description()} layout="inline">
				<ThemeModeSelector />
			</SettingsRow>
			<SettingsRow label={m.font_size()} description={m.font_size_description()} layout="inline">
				<FontSizePicker />
			</SettingsRow>
			<SettingsRow label={m.icon_catalog()} description={m.icon_catalog_description()} layout="inline">
				<div class="w-52">
					<SelectWithLabel
						id="account-icon-catalog"
						label={m.icon_catalog()}
						hideLabel
						triggerSize="sm"
						value={iconCatalogValue}
						options={iconCatalogOptions}
						onValueChange={(value) => void savePreferences({ iconCatalog: value as IconCatalog })}
					/>
				</div>
			</SettingsRow>
			<SettingsRow label={m.application_theme()} description={m.application_theme_description()}>
				<ApplicationThemePicker
					selectedTheme={applicationThemeValue}
					accentColor={accentColorValue}
					onSelect={(value) => savePreferencesDebounced({ applicationTheme: value })}
				/>
			</SettingsRow>
			<SettingsRow label={m.accent_color()} description={m.accent_color_description()}>
				<AccentColorPicker
					previousColor={accentColorValue}
					selectedColor={accentColorValue}
					onSelect={(value) => savePreferencesDebounced({ accentColor: value })}
				/>
			</SettingsRow>
			<SettingsRow label={m.oled_mode()} description={m.oled_mode_description()} layout="inline">
				{#snippet helpText()}
					{#if !isDefaultApplicationTheme}
						<p class="mt-1 text-xs text-muted-foreground/70 italic">{m.oled_mode_requires_default_theme()}</p>
					{:else if !isDarkMode}
						<p class="mt-1 text-xs text-muted-foreground/70 italic">{m.oled_mode_requires_dark()}</p>
					{/if}
				{/snippet}
				<Switch
					id="account-oled-mode"
					checked={oledModeEnabled}
					disabled={!isDefaultApplicationTheme}
					onCheckedChange={(checked) => {
						applyOledMode(checked);
						void savePreferences({ oledMode: checked });
					}}
				/>
			</SettingsRow>
			<SettingsRow label={m.glass_effects()} description={m.glass_effects_description()} layout="inline">
				<Switch
					id="account-glass-effects"
					checked={glassEffectsEnabled}
					onCheckedChange={(checked) => {
						applyGlassEffects(checked);
						void savePreferences({ glassEffectsEnabled: checked });
					}}
				/>
			</SettingsRow>
			<SettingsRow label={m.interface_animations()} description={m.interface_animations_description()} layout="inline">
				<Switch
					id="account-animations"
					checked={animationsEnabled}
					onCheckedChange={(checked) => {
						applyInterfaceAnimations(checked);
						void savePreferences({ animationsEnabled: checked });
					}}
				/>
			</SettingsRow>
		</div>
	</section>

	<section>
		<h3 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.navigation_title()}</h3>
		<div class="mt-4 divide-y divide-border/40 [&>*]:py-4 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0">
			<SettingsRow
				label={m.navigation_default_landing_page_label()}
				description={m.navigation_default_landing_page_description()}
				layout="inline"
			>
				<div class="w-52">
					<SelectWithLabel
						id="account-default-landing-page"
						label={m.navigation_default_landing_page_label()}
						hideLabel
						triggerSize="sm"
						value={landingValue}
						options={landingPageOptions}
						onValueChange={(value) => void savePreferences({ defaultLandingPage: value })}
					/>
				</div>
			</SettingsRow>
			<SettingsRow
				label={m.navigation_sidebar_hover_expansion_label()}
				description={m.navigation_sidebar_hover_expansion_description()}
				layout="inline"
			>
				<Switch
					id="account-sidebar-hover-expansion"
					checked={sidebarHoverExpansionEnabled}
					onCheckedChange={(checked) => void savePreferences({ sidebarHoverExpansion: checked })}
				/>
			</SettingsRow>
			<SettingsRow
				label={m.navigation_keyboard_shortcuts_label()}
				description={m.navigation_keyboard_shortcuts_description()}
				layout="inline"
			>
				<Switch
					id="account-keyboard-shortcuts"
					checked={keyboardShortcutsEnabled}
					onCheckedChange={(checked) => void savePreferences({ keyboardShortcutsEnabled: checked })}
				/>
			</SettingsRow>
		</div>
	</section>

	<section>
		<h3 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
			{m.navigation_mobile_appearance_title()}
		</h3>
		<div class="mt-4 divide-y divide-border/40 [&>*]:py-4 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0">
			<SettingsRow label={m.navigation_mode_label()} description={m.navigation_mode_description()} layout="inline">
				<div class="inline-flex rounded-lg bg-muted/40 p-0.5">
					{#each [{ value: 'floating' as const, label: m.navigation_mode_floating(), icon: MonitorSpeakerIcon }, { value: 'docked' as const, label: m.navigation_mode_docked(), icon: DockIcon }] as option (option.value)}
						<button
							type="button"
							aria-pressed={mobileNavigationMode === option.value}
							onclick={() => selectMobileNavigationMode(option.value)}
							class={cn(
								'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
								mobileNavigationMode === option.value
									? 'bg-background text-foreground shadow-sm'
									: 'text-muted-foreground hover:text-foreground'
							)}
						>
							<option.icon class="size-3.5" />
							{option.label}
						</button>
					{/each}
				</div>
			</SettingsRow>
			<SettingsRow label={m.navigation_show_labels_label()} description={m.navigation_show_labels_description()} layout="inline">
				<Switch
					id="account-mobile-nav-labels"
					checked={mobileNavigationShowLabels}
					onCheckedChange={(checked) => void savePreferences({ mobileNavigationShowLabels: checked })}
				/>
			</SettingsRow>
		</div>
	</section>
</div>
