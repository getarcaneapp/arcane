<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import * as Carousel from '$lib/components/ui/carousel/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import * as RadioGroup from '$lib/components/ui/radio-group/index.js';
	import { mode } from 'mode-watcher';
	import { m } from '$lib/paraglide/messages';
	import { APPLICATION_THEME_OPTIONS, applyApplicationTheme, resolveApplicationTheme } from '$lib/utils/application-theme-util';
	import type { CarouselAPI } from '$lib/components/ui/carousel/context.js';
	import type { ApplicationTheme } from '$lib/types/settings.type';

	let {
		selectedTheme = $bindable(),
		accentColor = '',
		disabled = false
	}: {
		selectedTheme: ApplicationTheme;
		accentColor?: string;
		disabled?: boolean;
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
		ocean: {
			label: m.application_theme_ocean(),
			description: m.application_theme_ocean_description()
		},
		amber: {
			label: m.application_theme_amber(),
			description: m.application_theme_amber_description()
		},
		github: {
			label: m.application_theme_github(),
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
	let hasInitializedCarouselPosition = $state(false);
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

	$effect(() => {
		if (!carouselApi) {
			return;
		}

		const api = carouselApi;

		const syncCenteredTheme = () => {
			const centeredTheme = APPLICATION_THEME_OPTIONS[api.selectedScrollSnap()]?.value;

			if (!centeredTheme || centeredTheme === selectedTheme) {
				return;
			}

			selectedTheme = centeredTheme;
			applyApplicationTheme(centeredTheme);
		};

		api.on('select', syncCenteredTheme);
		api.on('reInit', syncCenteredTheme);

		return () => {
			api.off('select', syncCenteredTheme);
			api.off('reInit', syncCenteredTheme);
		};
	});

	function handleThemeChange(value: string) {
		if (disabled) {
			return;
		}

		const nextTheme = resolveApplicationTheme(value);
		selectedTheme = nextTheme;
		applyApplicationTheme(nextTheme);
	}
</script>

<RadioGroup.Root class="space-y-3" value={selectedTheme} onValueChange={handleThemeChange}>
	<div class="bg-background/30 overflow-hidden rounded-xl border border-dashed p-2 sm:p-3">
		<Carousel.Root
			class="w-full"
			opts={{ align: 'center', loop: true }}
			setApi={(api) => {
				carouselApi = api;
			}}
		>
			<Carousel.Content>
				{#each APPLICATION_THEME_OPTIONS as theme (theme.value)}
					{@const option = themeCopy[theme.value]}
					{@const preview = isDarkMode ? theme.preview.dark : theme.preview.light}
					{@const previewAccentColor = accentColor.trim() || preview.primary}
					<Carousel.Item class="basis-[92%] sm:basis-[88%] lg:basis-[84%] xl:basis-[80%]">
						<div class="h-full p-1">
							<RadioGroup.Item id={`application-theme-${theme.value}`} value={theme.value} class="sr-only" {disabled} />
							<Label
								for={`application-theme-${theme.value}`}
								class={disabled ? 'cursor-not-allowed opacity-60' : 'cursor-pointer'}
							>
								<Card.Root
									variant="outlined"
									class={[
										'border-border/70 bg-card h-full transition-[border-color,background-color,opacity,filter] duration-200',
										selectedTheme === theme.value
											? 'border-primary bg-primary/5 shadow-sm'
											: 'hover:border-primary/40 opacity-85 saturate-75'
									]}
								>
									<Card.Content class="flex h-full flex-col gap-4 p-4">
										<div
											class="rounded-md border p-3"
											style={`background-color: ${preview.background}; border-color: ${preview.border};`}
										>
											<div class="flex gap-3">
												<div class="h-16 w-4 rounded-sm" style={`background-color: ${preview.sidebar};`}></div>
												<div class="min-w-0 flex-1 space-y-3">
													<div class="flex items-center justify-between gap-3">
														<div
															class="h-2 w-18 rounded-full opacity-80"
															style={`background-color: ${preview.foreground};`}
														></div>
														<div class="h-2 w-7 rounded-full" style={`background-color: ${previewAccentColor};`}></div>
													</div>
													<div class="grid grid-cols-2 gap-2">
														<div
															class="h-10 rounded-sm border"
															style={`background-color: ${preview.card}; border-color: ${preview.border};`}
														></div>
														<div
															class="h-10 rounded-sm border"
															style={`background-color: ${preview.card}; border-color: ${preview.border};`}
														></div>
													</div>
													<div class="flex gap-2">
														<div
															class="h-2 flex-1 rounded-full opacity-60"
															style={`background-color: ${preview.foreground};`}
														></div>
														<div
															class="h-2 w-10 rounded-full opacity-70"
															style={`background-color: ${preview.foreground};`}
														></div>
													</div>
												</div>
											</div>
										</div>

										<div class="space-y-1">
											<div class="flex items-center gap-2">
												<div class="size-2 rounded-full" style={`background-color: ${previewAccentColor};`}></div>
												<div class="text-sm font-medium">{option.label}</div>
											</div>
											<p class="text-muted-foreground text-xs leading-5">{option.description}</p>
										</div>
									</Card.Content>
								</Card.Root>
							</Label>
						</div>
					</Carousel.Item>
				{/each}
			</Carousel.Content>
			<div
				class="from-background/95 via-background/55 pointer-events-none absolute inset-y-2 start-0 z-10 w-10 bg-gradient-to-r to-transparent backdrop-blur-[2px] sm:w-14"
			></div>
			<div
				class="from-background/95 via-background/55 pointer-events-none absolute inset-y-2 end-0 z-10 w-10 bg-gradient-to-l to-transparent backdrop-blur-[2px] sm:w-14"
			></div>
			<Carousel.Previous class="start-3 z-20 hidden md:inline-flex" />
			<Carousel.Next class="end-3 z-20 hidden md:inline-flex" />
		</Carousel.Root>
	</div>
</RadioGroup.Root>
