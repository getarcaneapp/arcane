<script lang="ts">
	import { mode } from 'mode-watcher';

	import type { CarouselAPI } from '#lib/components/ui/carousel/context.js';
	import * as Carousel from '#lib/components/ui/carousel/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import { AddIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { ApplicationTheme, ThemeDefinition } from '#lib/types/settings.js';
	import { cn } from '#lib/utils.js';
	import {
		type ApplicationThemeOption,
		applyApplicationTheme,
		findApplicationTheme,
		getApplicationThemeOptions,
		resolveApplicationTheme
	} from '#lib/utils/theme.svelte.js';
	import { saveUserPreferences } from '#lib/utils/user-preferences.js';

	import ApplicationThemeCardMenu from './application-theme-card-menu.svelte';
	import ApplicationThemeImportDialog from './application-theme-import-dialog.svelte';

	let {
		selectedTheme = $bindable(),
		accentColor = '',
		disabled = false,
		onSelect
	}: {
		selectedTheme: ApplicationTheme;
		accentColor?: string;
		disabled?: boolean;
		onSelect?: (theme: ApplicationTheme) => void;
	} = $props();

	const themeCopy: Partial<Record<ApplicationTheme, { label: string; description: string }>> = {
		default: {
			label: m.application_theme_default(),
			description: m.application_theme_default_description()
		},
		graphite: {
			label: m.application_theme_graphite(),
			description: m.application_theme_graphite_description()
		},
		carbon: {
			label: m.application_theme_carbon(),
			description: m.application_theme_carbon_description()
		},
		ocean: {
			label: m.application_theme_ocean(),
			description: m.application_theme_ocean_description()
		},
		amber: {
			label: m.application_theme_amber(),
			description: m.application_theme_amber_description()
		},
		github: {
			label: m.common_github(),
			description: m.application_theme_github_description()
		},
		nord: {
			label: m.application_theme_nord(),
			description: m.application_theme_nord_description()
		},
		everforest: {
			label: m.application_theme_everforest(),
			description: m.application_theme_everforest_description()
		},
		rosepine: {
			label: m.application_theme_rosepine(),
			description: m.application_theme_rosepine_description()
		},
		abyss: {
			label: m.application_theme_abyss(),
			description: m.application_theme_abyss_description()
		},
		twilight: {
			label: m.application_theme_twilight(),
			description: m.application_theme_twilight_description()
		},
		neon: {
			label: m.application_theme_neon(),
			description: m.application_theme_neon_description()
		},
		mono: {
			label: m.application_theme_mono(),
			description: m.application_theme_mono_description()
		}
	};

	const themeOptions = $derived(getApplicationThemeOptions());
	const customThemes = $derived(userStore.current?.preferences?.customThemes ?? []);
	let importOpen = $state(false);

	function themeDescription(theme: ApplicationThemeOption): string {
		return themeCopy[theme.id]?.description ?? theme.description ?? '';
	}

	async function importTheme(theme: ThemeDefinition) {
		await saveUserPreferences({
			customThemes: [...customThemes, theme],
			applicationTheme: theme.id
		});
		if (customThemes.some((item) => item.id === theme.id)) {
			importOpen = false;
		}
	}
	const selectedDescription = $derived.by(() => {
		const selected = themeOptions.find((theme) => theme.id === selectedTheme);
		return selected ? themeDescription(selected) : '';
	});

	function themeLabel(theme: ApplicationThemeOption): string {
		return themeCopy[theme.id]?.label ?? theme.name ?? theme.id;
	}

	const isDarkMode = $derived(mode.current === 'dark');
	const customAccent = $derived(accentColor.trim() === '' || accentColor.trim() === 'theme' ? null : accentColor);

	function setCarouselApi(api: CarouselAPI | undefined) {
		api?.scrollTo(
			themeOptions.findIndex((theme) => theme.id === selectedTheme),
			true
		);
	}

	function handleThemeChange(value: string) {
		if (disabled) {
			return;
		}

		const nextTheme = resolveApplicationTheme(value);
		selectedTheme = nextTheme;
		applyApplicationTheme(nextTheme);
		onSelect?.(nextTheme);
	}
</script>

<RadioGroup.Root value={selectedTheme} onValueChange={handleThemeChange}>
	<Carousel.Root
		class="flex w-full items-center"
		opts={{ align: 'start', dragFree: true, containScroll: 'trimSnaps' }}
		setApi={setCarouselApi}
	>
		<Carousel.Previous class="static me-2 hidden shrink-0 translate-y-0 md:inline-flex" />
		<Carousel.Content>
			{#each themeOptions as theme (theme.id)}
				{@const preview = isDarkMode ? theme.dark : theme.light}
				{@const definition = findApplicationTheme(theme.id)}
				<Carousel.Item class="basis-32">
					<div class="group relative h-full">
						<RadioGroup.Item id={`application-theme-${theme.id}`} value={theme.id} class="sr-only" {disabled} />
						<Label for={`application-theme-${theme.id}`} class={disabled ? 'cursor-not-allowed' : 'cursor-pointer'}>
							<div
								class={cn(
									'flex w-full flex-col gap-1.5 rounded-lg border p-1.5 transition-colors',
									selectedTheme === theme.id ? 'border-primary ring-1 ring-primary' : 'hover:border-muted-foreground/40',
									disabled && 'opacity-60'
								)}
							>
								<div
									class="flex h-11 gap-1 rounded-md border border-(color:--preview-border) bg-(--preview-bg) p-1.5"
									style={`--preview-bg: ${preview.background}; --preview-border: ${preview.border}; --preview-sidebar: ${preview.sidebar}; --preview-fg: ${preview.foreground}; --preview-card: ${preview.card};`}
								>
									<div class="w-1.5 rounded-xs bg-(--preview-sidebar)"></div>
									<div class="flex min-w-0 flex-1 flex-col gap-1">
										<div class="h-0.75 w-1/2 rounded-full bg-(--preview-fg) opacity-70"></div>
										<div class="flex flex-1 gap-0.75">
											<div class="flex-1 rounded-xs bg-(--preview-card)"></div>
											<div class="flex-1 rounded-xs bg-(--preview-card)"></div>
										</div>
									</div>
								</div>
								<div class="flex items-center gap-1.5 px-0.5 text-xs font-medium">
									<span
										class="size-1.5 shrink-0 rounded-full bg-(--preview-accent)"
										style={`--preview-accent: ${customAccent ?? preview.primary}`}
									></span>
									<span class="truncate">{themeLabel(theme)}</span>
								</div>
							</div>
						</Label>
						{#if definition}
							<div
								class="absolute top-1 right-1 opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100 pointer-coarse:opacity-100"
							>
								<ApplicationThemeCardMenu
									theme={definition}
									label={themeLabel(theme)}
									description={themeDescription(theme)}
									custom={customThemes.some((item) => item.id === theme.id)}
								/>
							</div>
						{/if}
					</div>
				</Carousel.Item>
			{/each}
			<Carousel.Item class="basis-32">
				<button
					type="button"
					class="flex h-full w-full flex-col items-center justify-center gap-1 rounded-lg border border-dashed p-1.5 text-xs font-medium text-muted-foreground transition-colors hover:border-muted-foreground/40 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-60"
					{disabled}
					onclick={() => (importOpen = true)}
				>
					<AddIcon class="size-4" />
					{m.import_theme()}
				</button>
			</Carousel.Item>
		</Carousel.Content>
		<Carousel.Next class="static ms-2 hidden shrink-0 translate-y-0 md:inline-flex" />
	</Carousel.Root>
	<p class="text-xs text-muted-foreground">{selectedDescription}</p>
</RadioGroup.Root>

<ApplicationThemeImportDialog bind:open={importOpen} {customThemes} onSubmit={importTheme} />
