<script lang="ts">
	import { cn } from '#lib/utils.js';
	import { onDestroy } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Carousel from '#lib/components/ui/carousel/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import { mode } from 'mode-watcher';
	import { m } from '#lib/paraglide/messages.js';
	import { APPLICATION_THEME_OPTIONS, applyApplicationTheme, resolveApplicationTheme } from '#lib/utils/theme.svelte.js';
	import type { CarouselAPI } from '#lib/components/ui/carousel/context.js';
	import type { ApplicationTheme } from '#lib/types/settings.js';

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
		}
	};

	let carouselApi = $state<CarouselAPI | undefined>(undefined);
	let hasInitializedCarouselPosition = false;
	const isDarkMode = $derived(mode.current === 'dark');

	$effect(() => {
		const selectedIndex = APPLICATION_THEME_OPTIONS.findIndex((theme) => theme.value === selectedTheme);

		if (!carouselApi || selectedIndex < 0) {
			return;
		}

		if (carouselApi.selectedScrollSnap() === selectedIndex) {
			hasInitializedCarouselPosition = true;
			return;
		}

		carouselApi.scrollTo(selectedIndex, !hasInitializedCarouselPosition);
		hasInitializedCarouselPosition = true;
	});

	let releaseCarouselListeners: (() => void) | undefined;
	onDestroy(() => releaseCarouselListeners?.());

	function setCarouselApi(api: CarouselAPI | undefined) {
		releaseCarouselListeners?.();
		releaseCarouselListeners = undefined;
		carouselApi = api;
		if (!api) {
			return;
		}

		const syncCenteredTheme = () => {
			const centeredTheme = APPLICATION_THEME_OPTIONS[api.selectedScrollSnap()]?.value;

			if (!centeredTheme || centeredTheme === selectedTheme) {
				return;
			}

			selectedTheme = centeredTheme;
			applyApplicationTheme(centeredTheme);
			onSelect?.(centeredTheme);
		};

		api.on('select', syncCenteredTheme);
		api.on('reInit', syncCenteredTheme);

		releaseCarouselListeners = () => {
			api.off('select', syncCenteredTheme);
			api.off('reInit', syncCenteredTheme);
		};
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
	<div class="grid gap-6">
		<div class="overflow-hidden rounded-xl border border-dashed bg-background/30 p-2 sm:p-3">
			<Carousel.Root class="w-full" opts={{ align: 'center', loop: true }} setApi={setCarouselApi}>
				<Carousel.Content>
					{#each APPLICATION_THEME_OPTIONS as theme (theme.value)}
						{@const option = themeCopy[theme.value]}
						{@const preview = isDarkMode ? theme.preview.dark : theme.preview.light}
						{@const previewAccentColor =
							accentColor.trim() === '' || accentColor.trim() === 'theme' ? preview.primary : accentColor}
						<Carousel.Item class="basis-23/25 sm:basis-22/25 lg:basis-21/25 xl:basis-4/5">
							<div class="h-full p-1">
								<RadioGroup.Item id={`application-theme-${theme.value}`} value={theme.value} class="sr-only" {disabled} />
								<Label for={`application-theme-${theme.value}`} class={disabled ? 'cursor-not-allowed' : 'cursor-pointer'}>
									<div class={cn('h-full', selectedTheme !== theme.value && 'opacity-85 saturate-75', disabled && 'opacity-60')}>
										<Card.Root variant="outlined" interactive selected={selectedTheme === theme.value} class="h-full">
											<Card.Content class="h-full">
												<div class="flex h-full flex-col gap-4">
													<div
														class="rounded-md border border-(color:--preview-border) bg-(--preview-bg) p-3"
														style={`--preview-bg: ${preview.background}; --preview-border: ${preview.border}; --preview-sidebar: ${preview.sidebar}; --preview-fg: ${preview.foreground}; --preview-card: ${preview.card};`}
													>
														<div class="flex gap-3">
															<div class="h-16 w-4 rounded-sm bg-(--preview-sidebar)"></div>
															<div class="min-w-0 flex-1 space-y-3">
																<div class="flex items-center justify-between gap-3">
																	<div class="h-2 w-18 rounded-full bg-(--preview-fg) opacity-80"></div>
																	<div
																		class="h-2 w-7 rounded-full bg-(--preview-accent)"
																		style={`--preview-accent: ${previewAccentColor}`}
																	></div>
																</div>
																<div class="grid grid-cols-2 gap-2">
																	<div class="h-10 rounded-sm border border-(color:--preview-border) bg-(--preview-card)"></div>
																	<div class="h-10 rounded-sm border border-(color:--preview-border) bg-(--preview-card)"></div>
																</div>
																<div class="flex gap-2">
																	<div class="h-2 flex-1 rounded-full bg-(--preview-fg) opacity-60"></div>
																	<div class="h-2 w-10 rounded-full bg-(--preview-fg) opacity-70"></div>
																</div>
															</div>
														</div>
													</div>

													<div class="space-y-1">
														<div class="flex items-center gap-2">
															<div
																class="size-2 rounded-full bg-(--preview-accent)"
																style={`--preview-accent: ${previewAccentColor}`}
															></div>
															<div class="text-sm font-medium">{option.label}</div>
														</div>
														<p class="text-xs leading-5 text-muted-foreground">{option.description}</p>
													</div>
												</div>
											</Card.Content>
										</Card.Root>
									</div>
								</Label>
							</div>
						</Carousel.Item>
					{/each}
				</Carousel.Content>
				<div
					class="pointer-events-none absolute inset-y-2 start-0 z-(--arcane-z-raised) w-10 bg-gradient-to-r from-background/95 via-background/55 to-transparent backdrop-blur-2xs sm:w-14"
				></div>
				<div
					class="pointer-events-none absolute inset-y-2 end-0 z-(--arcane-z-raised) w-10 bg-gradient-to-l from-background/95 via-background/55 to-transparent backdrop-blur-2xs sm:w-14"
				></div>
				<Carousel.Previous class="start-3 z-(--arcane-z-sticky) hidden md:inline-flex" />
				<Carousel.Next class="end-3 z-(--arcane-z-sticky) hidden md:inline-flex" />
			</Carousel.Root>
		</div>
	</div>
</RadioGroup.Root>
