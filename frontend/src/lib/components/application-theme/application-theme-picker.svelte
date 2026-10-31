<script lang="ts">
	import { mode } from 'mode-watcher';

	import type { CarouselAPI } from '#lib/components/ui/carousel/context.js';
	import * as Carousel from '#lib/components/ui/carousel/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ApplicationTheme } from '#lib/types/settings.js';
	import { cn } from '#lib/utils.js';
	import { APPLICATION_THEME_OPTIONS, applyApplicationTheme, resolveApplicationTheme } from '#lib/utils/theme.svelte.js';

	let {
		selectedTheme = $bindable(),
		accentColor = '',
		disabled = false,
		onSelect
	}: {
		selectedTheme: ApplicationTheme;
		accentColor?: string;
		disabled?: boolean;
		/** Called whenever the user picks a different theme. */
		onSelect?: (theme: ApplicationTheme) => void;
	} = $props();

	const themeCopy: Record<ApplicationTheme, { label: string; description: string }> = {
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
		obsidian: {
			label: m.application_theme_obsidian(),
			description: m.application_theme_obsidian_description()
		},
		onyx: {
			label: m.application_theme_onyx(),
			description: m.application_theme_onyx_description()
		}
	};

	const isDarkMode = $derived(mode.current === 'dark');
	const customAccent = $derived(accentColor.trim() === '' || accentColor.trim() === 'theme' ? null : accentColor);

	function setCarouselApi(api: CarouselAPI | undefined) {
		api?.scrollTo(
			APPLICATION_THEME_OPTIONS.findIndex((theme) => theme.value === selectedTheme),
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
			{#each APPLICATION_THEME_OPTIONS as theme (theme.value)}
				{@const preview = isDarkMode ? theme.preview.dark : theme.preview.light}
				<Carousel.Item class="basis-32">
					<RadioGroup.Item id={`application-theme-${theme.value}`} value={theme.value} class="sr-only" {disabled} />
					<Label for={`application-theme-${theme.value}`} class={disabled ? 'cursor-not-allowed' : 'cursor-pointer'}>
						<div
							class={cn(
								'flex w-full flex-col gap-1.5 rounded-lg border p-1.5 transition-colors',
								selectedTheme === theme.value ? 'border-primary ring-1 ring-primary' : 'hover:border-muted-foreground/40',
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
								<span class="truncate">{themeCopy[theme.value].label}</span>
							</div>
						</div>
					</Label>
				</Carousel.Item>
			{/each}
		</Carousel.Content>
		<Carousel.Next class="static ms-2 hidden shrink-0 translate-y-0 md:inline-flex" />
	</Carousel.Root>
	<p class="text-xs text-muted-foreground">{themeCopy[selectedTheme].description}</p>
</RadioGroup.Root>
