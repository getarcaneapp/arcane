<script lang="ts">
	import { mode } from 'mode-watcher';

	import AccentColorPicker from '#lib/components/accent-color/accent-color-picker.svelte';
	import ApplicationThemePicker from '#lib/components/application-theme/application-theme-picker.svelte';
	import FontFamilyPicker from '#lib/components/font-family-picker.svelte';
	import FontSizePicker from '#lib/components/font-size-picker.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import LocalePicker from '#lib/components/locale-picker.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import ThemeModeSelector from '#lib/components/theme-mode/theme-mode-selector.svelte';
	import TimeFormatPicker from '#lib/components/time-format-picker.svelte';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { DEFAULT_LANDING_PAGE, getLandingPageNavItems } from '#lib/config/navigation-config.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLayoutMode, layoutModeStore, type LayoutMode } from '#lib/stores/layout-mode.store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { EditorTabWidth, ProjectEditorLayout, ToastPosition, UserPreferences } from '#lib/types/user.js';
	import type { ApplicationTheme, IconCatalog } from '#lib/types/user.js';
	import { applyAccentColor, applyGlassEffects, applyInterfaceAnimations, applyOledMode } from '#lib/utils/theme.svelte.js';
	import { saveUserPreferences } from '#lib/utils/user-preferences.js';
	import { debounced } from '#lib/utils/ws.js';

	import AllEnvironmentsPreferences from './all-environments-preferences.svelte';

	const currentUser = $derived(userStore.current);
	const preferences = $derived(currentUser?.preferences ?? {});
	const applicationThemeValue = $derived<ApplicationTheme>(preferences.applicationTheme ?? 'default');
	const accentColorValue = $derived(
		preferences.accentColor && preferences.accentColor !== 'default' ? preferences.accentColor : 'theme'
	);
	const iconCatalogValue = $derived(preferences.iconCatalog ?? 'selfhst');
	const oledModeEnabled = $derived(preferences.oledMode ?? false);
	const glassEffectsEnabled = $derived(preferences.glassEffectsEnabled ?? true);
	const animationsEnabled = $derived(preferences.animationsEnabled ?? true);
	const sidebarHoverExpansionEnabled = $derived(preferences.sidebarHoverExpansion ?? true);
	const keyboardShortcutsEnabled = $derived(preferences.keyboardShortcutsEnabled ?? true);
	const mobileNavigationShowLabels = $derived(preferences.mobileNavigationShowLabels ?? true);
	const projectEditorLayoutValue = $derived<ProjectEditorLayout>(preferences.defaultProjectEditorLayout ?? 'auto');
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
	const editorTabWidthOptions = $derived([2, 4, 8].map((count) => ({ value: String(count), label: m.spaces_count({ count }) })));
	const projectEditorLayoutOptions = $derived([
		{ value: 'auto', label: m.automatic(), description: m.project_editor_layout_auto_description() },
		{ value: 'classic', label: m.classic(), description: m.project_editor_layout_classic_description() },
		{ value: 'tree', label: m.tree(), description: m.project_editor_layout_tree_description() }
	]);
	// Device-local (localStorage), not part of the account preferences: a user may want
	// desktop layout on a tablet and automatic on a phone.
	const layoutModeValue = $derived<LayoutMode>(getLayoutMode());
	const layoutModeOptions = $derived([
		{ value: 'auto', label: m.automatic(), description: m.layout_mode_auto_description() },
		{ value: 'mobile', label: m.layout_mode_mobile(), description: m.layout_mode_mobile_description() },
		{ value: 'desktop', label: m.layout_mode_desktop(), description: m.layout_mode_desktop_description() }
	]);
	const toastPositionValue = $derived<ToastPosition>(preferences.toastPosition ?? 'auto');
	const toastPositionOptions = $derived([
		{ value: 'auto', label: m.automatic(), description: m.toast_position_auto_description() },
		{ value: 'top-left', label: m.toast_position_top_left() },
		{ value: 'top-center', label: m.toast_position_top_center() },
		{ value: 'top-right', label: m.toast_position_top_right() },
		{ value: 'bottom-left', label: m.toast_position_bottom_left() },
		{ value: 'bottom-center', label: m.toast_position_bottom_center() },
		{ value: 'bottom-right', label: m.toast_position_bottom_right() }
	]);
	const landingPageOptions = $derived(getLandingPageNavItems().map((item) => ({ value: item.url, label: item.title })));
	const landingValue = $derived.by(() => {
		const current = preferences.defaultLandingPage ?? DEFAULT_LANDING_PAGE;
		return landingPageOptions.some((option) => option.value === current) ? current : DEFAULT_LANDING_PAGE;
	});

	let pendingPreferences: Partial<UserPreferences> = {};
	const flushPreferences = debounced(() => {
		const patch = pendingPreferences;
		pendingPreferences = {};
		void saveUserPreferences(patch);
	}, 400);

	function savePreferencesDebounced(patch: Partial<UserPreferences>) {
		pendingPreferences = { ...pendingPreferences, ...patch };
		flushPreferences();
	}
</script>

<div class="space-y-8">
	<SettingsSection title={m.general_title()}>
		<SettingsRow label={m.language()} description={m.account_language_desc()} layout="switch">
			{#snippet children({ describedBy })}
				<LocalePicker inline id="accountLocalePicker" aria-describedby={describedBy} />
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.time_format()} description={m.account_time_format_desc()} layout="switch">
			{#snippet children({ describedBy })}
				<TimeFormatPicker id="accountTimeFormatPicker" aria-describedby={describedBy} />
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.project_editor_layout()} description={m.project_editor_layout_description()} layout="switch">
			{#snippet children({ describedBy })}
				<div class="w-52">
					<SelectWithLabel
						id="account-project-editor-layout"
						label={m.project_editor_layout()}
						hideLabel
						triggerSize="sm"
						value={projectEditorLayoutValue}
						options={projectEditorLayoutOptions}
						onValueChange={(value) => void saveUserPreferences({ defaultProjectEditorLayout: value as ProjectEditorLayout })}
						aria-describedby={describedBy}
					/>
				</div>
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.editor_tab_width()} description={m.editor_tab_width_description()} layout="switch">
			{#snippet children({ describedBy })}
				<div class="w-52">
					<SelectWithLabel
						id="account-editor-tab-width"
						label={m.editor_tab_width()}
						hideLabel
						triggerSize="sm"
						value={String(preferences.editorTabWidth ?? 2)}
						options={editorTabWidthOptions}
						onValueChange={(value) => void saveUserPreferences({ editorTabWidth: Number(value) as EditorTabWidth })}
						aria-describedby={describedBy}
					/>
				</div>
			{/snippet}
		</SettingsRow>
	</SettingsSection>

	<SettingsSection title={m.appearance_title()}>
		<SettingsRow label={m.account_theme()} description={m.appearance_theme_current_user_description()} layout="switch">
			{#snippet children({ describedBy })}
				<ThemeModeSelector aria-describedby={describedBy} />
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.font_size()} description={m.font_size_description()} layout="switch">
			{#snippet children({ describedBy })}
				<FontSizePicker aria-describedby={describedBy} />
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.interface_font()} description={m.interface_font_description()} layout="switch">
			{#snippet children({ describedBy })}
				<FontFamilyPicker kind="interface" aria-describedby={describedBy} />
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.monospace_font()} description={m.monospace_font_description()} layout="switch">
			{#snippet children({ describedBy })}
				<FontFamilyPicker kind="monospace" aria-describedby={describedBy} />
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.code_font()} description={m.code_font_description()} layout="switch">
			{#snippet children({ describedBy })}
				<FontFamilyPicker kind="code" aria-describedby={describedBy} />
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.layout_mode()} description={m.layout_mode_description()} layout="switch">
			{#snippet children({ describedBy })}
				<div class="w-52">
					<SelectWithLabel
						id="account-layout-mode"
						label={m.layout_mode()}
						hideLabel
						triggerSize="sm"
						value={layoutModeValue}
						options={layoutModeOptions}
						onValueChange={(value) => (layoutModeStore.current = value as LayoutMode)}
						aria-describedby={describedBy}
					/>
				</div>
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.toast_position()} description={m.toast_position_description()} layout="switch">
			{#snippet children({ describedBy })}
				<div class="w-52">
					<SelectWithLabel
						id="account-toast-position"
						label={m.toast_position()}
						hideLabel
						triggerSize="sm"
						value={toastPositionValue}
						options={toastPositionOptions}
						onValueChange={(value) => void saveUserPreferences({ toastPosition: value as ToastPosition })}
						aria-describedby={describedBy}
					/>
				</div>
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.icon_catalog()} description={m.icon_catalog_description()} layout="switch">
			{#snippet children({ describedBy })}
				<div class="w-52">
					<SelectWithLabel
						id="account-icon-catalog"
						label={m.icon_catalog()}
						hideLabel
						triggerSize="sm"
						value={iconCatalogValue}
						options={iconCatalogOptions}
						onValueChange={(value) => void saveUserPreferences({ iconCatalog: value as IconCatalog })}
						aria-describedby={describedBy}
					/>
				</div>
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.application_theme()} description={m.application_theme_description()} layout="wide">
			{#snippet children({ describedBy })}
				<ApplicationThemePicker
					selectedTheme={applicationThemeValue}
					accentColor={accentColorValue}
					onSelect={(value) => savePreferencesDebounced({ applicationTheme: value })}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.accent_color()} description={m.accent_color_description()} layout="wide">
			{#snippet children({ describedBy })}
				<AccentColorPicker
					previousColor={accentColorValue}
					selectedColor={accentColorValue}
					onSelect={(value) => {
						applyAccentColor(value);
						savePreferencesDebounced({ accentColor: value });
					}}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.oled_mode()} description={m.oled_mode_description()} layout="switch">
			{#snippet children({ describedBy })}
				<Switch
					id="account-oled-mode"
					checked={oledModeEnabled}
					disabled={!isDefaultApplicationTheme}
					onCheckedChange={(checked) => {
						applyOledMode(checked);
						void saveUserPreferences({ oledMode: checked });
					}}
					aria-describedby={describedBy}
				/>
			{/snippet}
			{#snippet helpText()}
				{#if !isDefaultApplicationTheme}
					<span class="italic">{m.oled_mode_requires_default_theme()}</span>
				{:else if !isDarkMode}
					<span class="italic">{m.oled_mode_requires_dark()}</span>
				{/if}
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.glass_effects()} description={m.glass_effects_description()} layout="switch">
			{#snippet children({ describedBy })}
				<Switch
					id="account-glass-effects"
					checked={glassEffectsEnabled}
					onCheckedChange={(checked) => {
						applyGlassEffects(checked);
						void saveUserPreferences({ glassEffectsEnabled: checked });
					}}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow label={m.interface_animations()} description={m.interface_animations_description()} layout="switch">
			{#snippet children({ describedBy })}
				<Switch
					id="account-animations"
					checked={animationsEnabled}
					onCheckedChange={(checked) => {
						applyInterfaceAnimations(checked);
						void saveUserPreferences({ animationsEnabled: checked });
					}}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
	</SettingsSection>

	<SettingsSection title={m.navigation_title()}>
		<SettingsRow
			label={m.navigation_default_landing_page_label()}
			description={m.navigation_default_landing_page_description()}
			layout="switch"
		>
			{#snippet children({ describedBy })}
				<div class="w-52">
					<SelectWithLabel
						id="account-default-landing-page"
						label={m.navigation_default_landing_page_label()}
						hideLabel
						triggerSize="sm"
						value={landingValue}
						options={landingPageOptions}
						onValueChange={(value) => void saveUserPreferences({ defaultLandingPage: value })}
						aria-describedby={describedBy}
					/>
				</div>
			{/snippet}
		</SettingsRow>
		<SettingsRow
			label={m.navigation_sidebar_hover_expansion_label()}
			description={m.navigation_sidebar_hover_expansion_description()}
			layout="switch"
		>
			{#snippet children({ describedBy })}
				<Switch
					id="account-sidebar-hover-expansion"
					checked={sidebarHoverExpansionEnabled}
					onCheckedChange={(checked) => void saveUserPreferences({ sidebarHoverExpansion: checked })}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow
			label={m.navigation_keyboard_shortcuts_label()}
			description={m.navigation_keyboard_shortcuts_description()}
			layout="switch"
		>
			{#snippet children({ describedBy })}
				<Switch
					id="account-keyboard-shortcuts"
					checked={keyboardShortcutsEnabled}
					onCheckedChange={(checked) => void saveUserPreferences({ keyboardShortcutsEnabled: checked })}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
	</SettingsSection>

	<SettingsSection title={m.navigation_mobile_appearance_title()}>
		<SettingsRow label={m.navigation_show_labels_label()} description={m.navigation_show_labels_description()} layout="switch">
			{#snippet children({ describedBy })}
				<Switch
					id="account-mobile-nav-labels"
					checked={mobileNavigationShowLabels}
					onCheckedChange={(checked) => void saveUserPreferences({ mobileNavigationShowLabels: checked })}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
	</SettingsSection>

	<AllEnvironmentsPreferences />
</div>
