<script lang="ts">
	import { browser } from '$app/env';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SearchableSelect from '#lib/components/form/searchable-select.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { SearchIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { UserPreferences } from '#lib/types/user.js';
	import { toFontFamily, type FontSlot } from '#lib/utils/theme.svelte.js';
	import { saveUserPreferences } from '#lib/utils/user-preferences.js';

	type FontPreferenceKey = keyof Pick<UserPreferences, 'interfaceFont' | 'monospaceFont' | 'codeFont'>;
	type LocalFontWindow = Window & { queryLocalFonts?: () => Promise<{ family: string }[]> };

	const PREFERENCE_KEYS: Record<FontSlot, FontPreferenceKey> = {
		interface: 'interfaceFont',
		monospace: 'monospaceFont',
		code: 'codeFont'
	};
	const LABELS: Record<FontSlot, () => string> = {
		interface: m.interface_font,
		monospace: m.monospace_font,
		code: m.code_font
	};
	const PREVIEW_CLASSES: Record<FontSlot, string> = {
		interface: 'font-sans',
		monospace: 'font-mono',
		code: 'font-code'
	};
	const FONT_NAME_MAX_LENGTH = 128;
	const CUSTOM_VALUE = 'custom';

	let {
		kind,
		'aria-describedby': ariaDescribedby
	}: {
		kind: FontSlot;
		'aria-describedby'?: string;
	} = $props();

	const id = $props.id();
	const label = $derived(LABELS[kind]());
	const preferenceKey = $derived(PREFERENCE_KEYS[kind]);
	const storedFont = $derived(userStore.current?.preferences?.[preferenceKey]?.trim() ?? '');
	const options = $derived([
		{ label: m.default_font(), value: '' },
		{ label: m.system_ui(), value: 'system-ui, sans-serif' },
		{ label: m.montserrat(), value: "'Montserrat Variable'" },
		{ label: m.geist_mono(), value: "'Geist Mono Variable'" },
		{ label: m.custom(), value: CUSTOM_VALUE }
	]);
	const isPreset = $derived(options.some((option) => option.value !== CUSTOM_VALUE && option.value === storedFont));

	let customMode = $derived(!isPreset);
	let customFont = $derived(isPreset ? '' : storedFont);

	const customError = $derived(isValidFontName(customFont.trim()) ? null : m.font_name_invalid());
	const previewFamily = $derived(customMode && !customError ? toFontFamily(customFont) : null);

	const canBrowseFonts = browser && 'queryLocalFonts' in window;
	let installedFonts = $state<string[] | null>(null);
	let loadingFonts = $state(false);
	const installedFontItems = $derived(installedFonts?.map((family) => ({ value: family, label: family })) ?? []);

	function isValidFontName(name: string) {
		return name.length <= FONT_NAME_MAX_LENGTH && (name === '' || toFontFamily(name) !== null);
	}

	async function loadInstalledFonts() {
		loadingFonts = true;
		try {
			const fonts = (await (window as LocalFontWindow).queryLocalFonts?.()) ?? [];
			installedFonts = [...new Set(fonts.map((font) => font.family))].filter(isValidFontName).sort((a, b) => a.localeCompare(b));
		} catch (error) {
			toast.error(m.installed_fonts_unavailable(), { description: error instanceof Error ? error.message : undefined });
		} finally {
			loadingFonts = false;
		}
	}

	function selectInstalledFont(family: string) {
		customFont = family;
		saveCustomFont();
	}

	function save(value: string) {
		if (value === storedFont) return;
		void saveUserPreferences({ [preferenceKey]: value });
	}

	function handleSelect(value: string) {
		customMode = value === CUSTOM_VALUE;
		if (!customMode) save(value);
	}

	function saveCustomFont() {
		if (customError) return;
		save(customFont.trim());
	}

	function handleCustomKeydown(event: KeyboardEvent) {
		if (event.key === 'Enter') saveCustomFont();
	}
</script>

<div class="flex w-52 flex-col gap-2">
	<SelectWithLabel
		{id}
		{label}
		hideLabel
		triggerSize="sm"
		value={customMode ? CUSTOM_VALUE : storedFont}
		{options}
		onValueChange={handleSelect}
		aria-describedby={ariaDescribedby}
	/>
	{#if customMode}
		<Input
			id={`${id}-custom`}
			bind:value={customFont}
			maxlength={FONT_NAME_MAX_LENGTH}
			placeholder={m.custom_font_placeholder()}
			aria-label={m.custom_font()}
			aria-invalid={!!customError}
			aria-errormessage={customError ? `${id}-error` : undefined}
			class="h-8"
			onblur={saveCustomFont}
			onkeydown={handleCustomKeydown}
		/>
		{#if installedFonts}
			<SearchableSelect
				triggerId={`${id}-installed`}
				items={installedFontItems}
				value={customFont.trim()}
				selectText={m.installed_fonts()}
				onSelect={selectInstalledFont}
				showCheckboxes={false}
				class="w-full"
			/>
		{:else if canBrowseFonts}
			<ArcaneButton
				action="base"
				tone="outline"
				icon={SearchIcon}
				customLabel={m.browse_installed_fonts()}
				loading={loadingFonts}
				disabled={loadingFonts}
				onclick={loadInstalledFonts}
			/>
		{/if}
		{#if customError}
			<p id={`${id}-error`} class="text-xs font-medium text-destructive">{customError}</p>
		{/if}
	{/if}
	<p
		class={[
			'truncate text-sm text-muted-foreground',
			previewFamily ? 'font-(family-name:--preview-font)' : PREVIEW_CLASSES[kind]
		]}
		style:--preview-font={previewFamily && `${previewFamily}, var(--font-cjk)`}
	>
		{m.font_preview_sample()}
	</p>
</div>
