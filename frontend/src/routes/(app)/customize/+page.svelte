<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';

	import CategoryIndexPage from '#lib/components/category-index-page.svelte';
	import type { NormalizedCategory } from '#lib/components/category-index-page.types.js';
	import { getCustomizeSubpageUrlsInNavOrder } from '#lib/config/navigation-config.js';
	import { useCategorySearch } from '#lib/hooks/use-category-search.svelte.js';
	import { TemplateIcon, FileTextIcon, RegistryIcon, VariableIcon, CustomizeIcon, GitBranchIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { customizeSearchService } from '#lib/services/customize-search.js';
	import type { CustomizeCategory } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { canReachAccessSurfaceUrl } from '#lib/utils/auth.js';
	import { normalizeCategory, orderCategoriesByNav } from '#lib/utils/category-page.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import type { PageProps } from './$types';

	let { data }: PageProps = $props();
	let customizeCategories = $state<CustomizeCategory[]>([]);
	const user = $derived(data.user);
	const permissionsManifest = $derived(data.permissionsManifest);
	const categorySearch = useCategorySearch<CustomizeCategory>({
		search: (query) => customizeSearchService.search(query),
		filter: isAccessibleCategory
	});

	const iconMap: Record<string, any> = {
		'file-text': FileTextIcon,
		layers: TemplateIcon,
		package: RegistryIcon,
		code: VariableIcon,
		'git-branch': GitBranchIcon
	};

	const categoryMessages = {
		templates: {
			title: m.templates_title,
			description: m.templates_subtitle
		},
		registries: {
			title: m.registries_title,
			description: m.registries_subtitle
		},
		variables: {
			title: m.variables_title,
			description: m.variables_subtitle
		},
		'git-repositories': {
			title: m.git_repositories_title,
			description: m.git_repositories_subtitle
		}
	} as const;

	function isAccessibleCategory(category: CustomizeCategory) {
		if (!permissionsManifest?.accessSurfaces?.length) return true;
		return canReachAccessSurfaceUrl(permissionsManifest, category.url, user);
	}

	onMount(async () => {
		await handleApiResultWithCallbacks({
			result: await tryCatch(customizeSearchService.getCategories()),
			message: m.common_load_failed({ resource: m.customize_title() }),
			onSuccess: (categories) => {
				customizeCategories = orderCategoriesByNav(categories.filter(isAccessibleCategory), getCustomizeSubpageUrlsInNavOrder());
			}
		});
	});

	function navigateToCategory(categoryUrl: string) {
		goto(categoryUrl);
	}

	function normalize(category: CustomizeCategory): NormalizedCategory {
		return {
			...normalizeCategory(category, categoryMessages[category.id as keyof typeof categoryMessages], iconMap, CustomizeIcon),
			matchingItems: category.matchingCustomizations
		};
	}

	const normalizedCategories = $derived(customizeCategories.map(normalize));
	const searchAdapter = {
		get searchQuery() {
			return categorySearch.searchQuery;
		},
		set searchQuery(value: string) {
			categorySearch.searchQuery = value;
		},
		get showSearchResults() {
			return categorySearch.showSearchResults;
		},
		get searchResults() {
			return categorySearch.searchResults.map(normalize);
		},
		get isSearching() {
			return categorySearch.isSearching;
		},
		get searchError() {
			return categorySearch.searchError;
		},
		performSearch: categorySearch.performSearch,
		debouncedSearch: categorySearch.debouncedSearch,
		clearSearch: categorySearch.clearSearch
	};
</script>

<CategoryIndexPage
	headerIcon={CustomizeIcon}
	title={m.customize_title()}
	subtitle={m.customize_subtitle()}
	searchPlaceholder={m.customize_search_placeholder()}
	clearSearchLabel={m.common_clear_search()}
	searchingLabel={m.searching()}
	searchFailedLabel={m.common_search_failed()}
	noResultsTitle={m.customize_no_options()}
	noResultsDescription={m.customize_try_adjusting()}
	matchingItemsLabel={m.customize_available_options()}
	goToPageLabel={m.customize_button()}
	categories={normalizedCategories}
	categorySearch={searchAdapter}
	navigate={navigateToCategory}
>
	{#snippet resultsHeading()}
		{m.customize_search_results({ query: categorySearch.searchQuery })} ({categorySearch.searchResults.length}
		{categorySearch.searchResults.length === 1 ? m.customize_result() : m.customize_results()})
	{/snippet}
	{#snippet moreKeywords(count: number)}
		+{count}
		{m.customize_more()}
	{/snippet}
</CategoryIndexPage>
