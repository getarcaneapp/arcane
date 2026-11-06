<script lang="ts">
	import type { Snippet } from 'svelte';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { SettingLock } from '#lib/components/badges/index.js';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Card } from '#lib/components/ui/card/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { SearchIcon, ArrowRightIcon, CloseIcon } from '#lib/icons/index.js';

	import type { CategoryGroup, NormalizedCategory } from './category-index-page.types';

	interface CategorySearch {
		searchQuery: string;
		showSearchResults: boolean;
		searchResults: NormalizedCategory[];
		isSearching: boolean;
		searchError: string | null;
		performSearch: (query: string) => void | Promise<void>;
		debouncedSearch: (query: string) => void;
		clearSearch: () => void;
	}

	interface Props {
		title: string;
		subtitle: string;
		searchPlaceholder: string;
		clearSearchLabel: string;
		searchingLabel: string;
		searchFailedLabel: string;
		noResultsTitle: string;
		noResultsDescription: string;
		matchingItemsLabel: string;
		goToPageLabel: string;
		groups: CategoryGroup[];
		categorySearch: CategorySearch;
		resultsHeading: Snippet;
		moreKeywords: Snippet<[number]>;
	}

	let {
		title,
		subtitle,
		searchPlaceholder,
		clearSearchLabel,
		searchingLabel,
		searchFailedLabel,
		noResultsTitle,
		noResultsDescription,
		matchingItemsLabel,
		goToPageLabel,
		groups,
		categorySearch,
		resultsHeading,
		moreKeywords
	}: Props = $props();
</script>

<div class="space-y-6 pb-5 md:space-y-8 md:pb-5">
	<header class="space-y-4 pt-3 sm:space-y-6 md:pt-5">
		<div class="flex items-start justify-between gap-4">
			<div class="min-w-0">
				<h1 class="text-xl font-semibold tracking-tight sm:text-2xl">{title}</h1>
				<p class="mt-1 text-sm text-muted-foreground">{subtitle}</p>
			</div>
			<SettingLock />
		</div>

		<div class="relative w-full sm:max-w-md">
			<InputGroup.Root>
				<InputGroup.Input
					placeholder={searchPlaceholder}
					value={categorySearch.searchQuery}
					oninput={(e) => {
						categorySearch.searchQuery = e.currentTarget.value;
						categorySearch.debouncedSearch(e.currentTarget.value);
					}}
					onkeydown={(e) => {
						if (e.key === 'Enter') {
							categorySearch.performSearch((e.currentTarget as HTMLInputElement).value);
						}
					}}
				/>
				<InputGroup.Addon>
					{#if categorySearch.showSearchResults}
						<ArcaneButton
							action="base"
							tone="ghost"
							size="icon"
							onclick={categorySearch.clearSearch}
							class="size-6"
							icon={CloseIcon}
							showLabel={false}
							customLabel={clearSearchLabel}
						/>
					{:else}
						<SearchIcon class="size-4" />
					{/if}
				</InputGroup.Addon>
			</InputGroup.Root>
		</div>
	</header>

	{#if !categorySearch.showSearchResults}
		<div class="space-y-6">
			{#each groups as group (group.id)}
				<SettingsSection title={group.title}>
					{#each group.categories as category (category.id)}
						{@const Icon = category.icon}
						<a
							href={category.href}
							class="flex w-full cursor-pointer items-center gap-3 hover-glow px-4 py-3 text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none focus-visible:ring-inset sm:gap-4"
						>
							<span
								class="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary-text ring-1 ring-primary/20 ring-inset"
							>
								<Icon class="size-4" />
							</span>
							<span class="min-w-0 flex-1">
								<span class="block truncate text-sm font-semibold">{category.title}</span>
								<span class="mt-0.5 line-clamp-2 text-xs-plus leading-5 text-muted-foreground">{category.description}</span>
							</span>
							<ArrowRightIcon class="size-4 shrink-0 text-muted-foreground" />
						</a>
					{/each}
				</SettingsSection>
			{/each}
		</div>
	{:else}
		<div class="space-y-6 sm:space-y-8">
			<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
				<h2 class="text-base font-semibold sm:text-lg">
					{@render resultsHeading()}
				</h2>
			</div>

			{#if categorySearch.isSearching}
				<div class="py-8 text-center sm:py-12">
					<Spinner tone="primary" class="mx-auto mb-3 size-8 sm:mb-4 sm:size-12" />
					<p class="text-sm text-muted-foreground sm:text-base">{searchingLabel}</p>
				</div>
			{:else if categorySearch.searchError}
				<div class="py-8 text-center sm:py-12">
					<SearchIcon class="mx-auto mb-3 size-8 text-destructive sm:mb-4 sm:size-12" />
					<h3 class="mb-2 text-base font-medium sm:text-lg">{searchFailedLabel}</h3>
					<p class="text-sm text-muted-foreground sm:text-base">{categorySearch.searchError}</p>
				</div>
			{:else if categorySearch.searchResults.length === 0}
				<div class="py-8 text-center sm:py-12">
					<SearchIcon class="mx-auto mb-3 size-8 text-muted-foreground sm:mb-4 sm:size-12" />
					<h3 class="mb-2 text-base font-medium sm:text-lg">{noResultsTitle}</h3>
					<p class="text-sm text-muted-foreground sm:text-base">{noResultsDescription}</p>
				</div>
			{:else}
				<div class="space-y-4">
					{#each categorySearch.searchResults as result (result.id)}
						{@const Icon = result.icon}
						<Card>
							<div class="flex items-center justify-between gap-3 border-b border-border/70 p-4 sm:p-5">
								<div class="flex min-w-0 items-center gap-3 sm:gap-4">
									<span
										class="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary-text ring-1 ring-primary/20 ring-inset"
									>
										<Icon class="size-5" />
									</span>
									<div class="min-w-0">
										<h3 class="truncate text-sm leading-tight font-semibold sm:text-base">{result.title}</h3>
										<p class="mt-1 line-clamp-2 text-xs leading-relaxed text-muted-foreground sm:text-sm">
											{result.description}
										</p>
									</div>
								</div>
								<ArcaneButton action="base" tone="outline" href={result.href} class="shrink-0" customLabel={goToPageLabel} />
							</div>

							{#if result.matchingItems && result.matchingItems.length > 0}
								<div class="space-y-2 p-4 sm:p-5">
									<h4 class="text-2xs font-medium tracking-widest text-muted-foreground uppercase">
										{matchingItemsLabel}
									</h4>
									{#each result.matchingItems as item (item.key)}
										<div class="rounded-lg border border-border/70 bg-card/10 p-3">
											<div class="flex items-start justify-between gap-3">
												<div class="min-w-0 flex-1">
													<h5 class="text-sm font-medium">{item.label}</h5>
													{#if item.description}
														<p class="mt-1 text-xs text-muted-foreground">{item.description}</p>
													{/if}
													{#if item.keywords && item.keywords.length > 0}
														<div class="mt-2 flex flex-wrap gap-1">
															{#each item.keywords.slice(0, 6) as keyword (keyword)}
																<span class="rounded-xl bg-foreground/5 px-2 py-0.5 text-xs text-muted-foreground">
																	{keyword}
																</span>
															{/each}
															{#if item.keywords.length > 6}
																<span class="px-2 py-0.5 text-xs text-muted-foreground">
																	{@render moreKeywords(item.keywords.length - 6)}
																</span>
															{/if}
														</div>
													{/if}
												</div>
												<span
													class="shrink-0 rounded-xl bg-foreground/5 px-2 py-1 font-mono text-2xs text-muted-foreground ring-1 ring-border/70 ring-inset"
												>
													{item.type}
												</span>
											</div>
										</div>
									{/each}
								</div>
							{/if}
						</Card>
					{/each}
				</div>
			{/if}
		</div>
	{/if}
</div>
