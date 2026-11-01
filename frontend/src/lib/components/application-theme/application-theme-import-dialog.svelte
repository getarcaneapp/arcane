<script lang="ts">
	import GitopsDialogFooter from '#lib/components/dialogs/gitops-dialog-footer.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import { FileDropZone, type FileDropZoneProps } from '#lib/components/ui/file-drop-zone/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { THEME_TOKENS, type ThemeDefinition, type ThemePalette } from '#lib/types/settings.js';
	import { trySync } from '#lib/utils/try-catch.js';

	type ThemeField = 'json' | 'id' | 'name' | 'light' | 'dark';

	const MAX_CUSTOM_THEMES = 20;
	const MAX_LENGTH = 48;
	const ID_CHARACTERS = 'abcdefghijklmnopqrstuvwxyz0123456789-';
	const HEX_CHARACTERS = '0123456789abcdefABCDEF';
	const COLOR_FUNCTIONS = ['rgb(', 'rgba(', 'hsl(', 'hsla(', 'oklch(', 'oklab('];
	const FUNCTION_CHARACTERS = `${ID_CHARACTERS}ABCDEFGHIJKLMNOPQRSTUVWXYZ%., /`;

	let {
		open = $bindable(false),
		customThemes,
		onSubmit
	}: {
		open: boolean;
		customThemes: readonly ThemeDefinition[];
		onSubmit: (theme: ThemeDefinition) => Promise<boolean>;
	} = $props();

	let jsonContent = $state('');
	let errors = $state<Partial<Record<ThemeField, string[]>>>({});
	let isLoading = $state(false);

	const atLimit = $derived(customThemes.length >= MAX_CUSTOM_THEMES);
	const fieldLabels: Record<ThemeField, string> = {
		json: m.git_sync_import_content_label(),
		id: m.common_id(),
		name: m.common_name(),
		light: m.sidebar_light_mode(),
		dark: m.sidebar_dark_mode()
	};

	function isRecord(value: unknown): value is Record<string, unknown> {
		return typeof value === 'object' && value !== null && !Array.isArray(value);
	}

	function slugify(value: string): string {
		return value
			.toLowerCase()
			.split('')
			.map((character) => (ID_CHARACTERS.includes(character) ? character : '-'))
			.join('')
			.split('-')
			.filter(Boolean)
			.join('-');
	}

	function isSafeColor(value: unknown): value is string {
		if (typeof value !== 'string' || !CSS.supports('color', value)) {
			return false;
		}
		if (value.startsWith('#')) {
			return (
				value.length >= 4 && value.length <= 9 && [...value].slice(1).every((character) => HEX_CHARACTERS.includes(character))
			);
		}
		const colorFunction = COLOR_FUNCTIONS.find((prefix) => value.startsWith(prefix));
		const inner = value.slice(colorFunction?.length ?? 0, -1);
		return (
			colorFunction !== undefined &&
			value.endsWith(')') &&
			inner.length >= 1 &&
			inner.length <= 64 &&
			[...inner].every((character) => FUNCTION_CHARACTERS.includes(character))
		);
	}

	function paletteErrors(value: unknown): string[] {
		const palette = isRecord(value) ? value : {};
		const missing = THEME_TOKENS.filter((token) => palette[token] === undefined);
		const invalid = THEME_TOKENS.filter((token) => palette[token] !== undefined && !isSafeColor(palette[token]));
		return [
			...(missing.length > 0 ? [m.theme_missing_tokens({ tokens: missing.join(', ') })] : []),
			...(invalid.length > 0 ? [m.theme_invalid_colors({ tokens: invalid.join(', ') })] : [])
		];
	}

	function pickPalette(value: Record<string, unknown>): ThemePalette {
		return Object.fromEntries(THEME_TOKENS.map((token) => [token, String(value[token])])) as ThemePalette;
	}

	function parseTheme(): ThemeDefinition | null {
		if (!jsonContent.trim()) {
			errors = { json: [m.git_sync_import_json_required()] };
			return null;
		}

		const parsed = trySync(() => JSON.parse(jsonContent) as unknown);
		if (parsed.error !== null) {
			errors = { json: [m.git_sync_import_invalid_json({ error: parsed.error.message })] };
			return null;
		}

		if (!isRecord(parsed.data)) {
			errors = { json: [m.theme_json_object_required()] };
			return null;
		}

		const { id: rawId, name: rawName, description, light, dark } = parsed.data;
		const name = typeof rawName === 'string' ? rawName.trim() : '';
		const id = typeof rawId === 'string' && rawId.startsWith('custom-') ? rawId : `custom-${slugify(name)}`.slice(0, MAX_LENGTH);
		const idErrors = [
			...(id.length > MAX_LENGTH || id.split('').some((character) => !ID_CHARACTERS.includes(character))
				? [m.theme_id_invalid()]
				: []),
			...(customThemes.some((theme) => theme.id === id) ? [m.theme_id_in_use({ id })] : [])
		];
		const nameLength = [...name].length;

		errors = Object.fromEntries(
			Object.entries({
				id: idErrors,
				name: nameLength === 0 || nameLength > MAX_LENGTH ? [m.theme_name_length()] : [],
				light: paletteErrors(light),
				dark: paletteErrors(dark)
			}).filter(([, messages]) => messages.length > 0)
		);

		if (Object.keys(errors).length > 0 || !isRecord(light) || !isRecord(dark)) {
			return null;
		}

		return {
			id,
			name,
			...(typeof description === 'string' && description.trim() !== '' ? { description: description.trim() } : {}),
			light: pickPalette(light),
			dark: pickPalette(dark)
		};
	}

	function close() {
		open = false;
		jsonContent = '';
		errors = {};
	}

	async function handleSubmit() {
		const theme = parseTheme();
		if (!theme) {
			return;
		}

		isLoading = true;
		const saved = await onSubmit(theme);
		isLoading = false;
		if (saved) {
			close();
		}
	}

	const onUpload: FileDropZoneProps['onUpload'] = async (files) => {
		const file = files[0];
		if (file) {
			jsonContent = await file.text();
			errors = {};
		}
	};

	const onFileRejected: FileDropZoneProps['onFileRejected'] = () => {
		errors = { json: [m.theme_json_file_required()] };
	};
</script>

<ResponsiveDialog
	{open}
	onOpenChange={(nextOpen) => {
		if (!nextOpen) {
			close();
		}
	}}
	title={m.import_theme()}
	description={m.import_theme_description()}
	contentClass="sm:max-w-2xl"
>
	{#snippet children()}
		<div class="grid gap-y-3 py-4">
			{#if atLimit}
				<p class="text-sm text-destructive">{m.theme_limit_reached({ max: MAX_CUSTOM_THEMES })}</p>
			{/if}
			<div class="space-y-1.5">
				<Label for="theme-json-file">{m.git_sync_import_upload_label()}</Label>
				<FileDropZone
					id="theme-json-file"
					accept=".json,application/json"
					multiple={false}
					disabled={atLimit || isLoading}
					{onUpload}
					{onFileRejected}
				/>
			</div>

			<div class="space-y-1.5">
				<Label for="theme-json-content">{m.git_sync_import_content_label()}</Label>
				<Textarea
					id="theme-json-content"
					bind:value={jsonContent}
					placeholder={`{\n  "id": "custom-my-theme",\n  "name": "My Theme",\n  "light": { "background": "#ffffff", ... },\n  "dark": { "background": "#18181b", ... }\n}`}
					mono
					size="sm"
					rows={12}
					disabled={atLimit || isLoading}
				/>
			</div>

			{#if Object.keys(errors).length > 0}
				<ul class="space-y-1 text-sm text-destructive">
					{#each Object.entries(errors) as [field, messages] (field)}
						{#each messages as message (message)}
							<li><span class="font-medium">{fieldLabels[field as ThemeField]}:</span> {message}</li>
						{/each}
					{/each}
				</ul>
			{/if}
		</div>
	{/snippet}

	{#snippet footer()}
		<GitopsDialogFooter cancelLabel={m.common_cancel()} {isLoading} onCancel={close}>
			{#snippet primary()}
				<Button onclick={handleSubmit} class="flex-1" disabled={atLimit || isLoading}>
					{#if isLoading}
						<Spinner class="mr-2 size-4" />
					{/if}
					{m.import()}
				</Button>
			{/snippet}
		</GitopsDialogFooter>
	{/snippet}
</ResponsiveDialog>
