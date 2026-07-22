<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { m } from '$lib/paraglide/messages';
	import { customizeSearchService } from '$lib/services/customize-search';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import type { CustomizeCategory } from '$lib/types/shared';
	import { canReachAccessSurfaceUrl } from '$lib/utils/access-policy';
	import { getCustomizeSubpageUrlsInNavOrder } from '$lib/config/navigation-config';
	import { useCategorySearch } from '$lib/hooks/use-category-search.svelte';
	import { getCategoryIcon, orderCategoriesByNav } from '$lib/utils/category-page';
	import { TemplateIcon, FileTextIcon, RegistryIcon, VariableIcon, CustomizeIcon, GitBranchIcon } from '$lib/icons';
	import CategoryIndexPage from '$lib/components/category-index-page.svelte';
	import type { NormalizedCategory } from '$lib/components/category-index-page.types';

	let { data }: PageProps = $props();
	let customizeCategories = $state<CustomizeCategory[]>([]);
	const user = $derived(data.user);
	const permissionsManifest = $derived(data.permissionsManifest);
	const categorySearch = useCategorySearch<CustomizeCategory>({
		search: (query) => customizeSearchService.search(query),
		filter: isAccessibleCategory,
		onError: (error) => console.error('Search failed:', error)
	});

	const iconMap: Record<string, any> = {
		'file-text': FileTextIcon,
		layers: TemplateIcon,
		package: RegistryIcon,
		code: VariableIcon,
		'git-branch': GitBranchIcon
	};

	function isAccessibleCategory(category: CustomizeCategory) {
		if (!permissionsManifest?.accessSurfaces?.length) return true;
		return canReachAccessSurfaceUrl(permissionsManifest, category.url, user, environmentStore.selected?.id);
	}

	onMount(async () => {
		try {
			customizeCategories = orderCategoriesByNav(
				(await customizeSearchService.getCategories()).filter(isAccessibleCategory),
				getCustomizeSubpageUrlsInNavOrder()
			);
		} catch (error) {
			console.error('Failed to load categories:', error);
		}
	});

	function navigateToCategory(categoryUrl: string) {
		goto(categoryUrl);
	}

	function getIconComponent(iconName: string) {
		return getCategoryIcon(iconMap, iconName, CustomizeIcon);
	}

	function normalize(category: CustomizeCategory): NormalizedCategory {
		return {
			id: category.id,
			title: category.title,
			description: category.description,
			icon: getIconComponent(category.icon),
			href: category.url,
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
	noResultsTitle={m.customize_no_options()}
	noResultsDescription={m.customize_try_adjusting()}
	matchingItemsLabel={m.customize_available_options()}
	goToPageLabel={m.customize_button()}
	rootClass="space-y-8 pb-5 md:space-y-10 md:pb-5"
	cardClass="hover:border-primary/20 group cursor-pointer transition-all duration-200 hover:shadow-md"
	resultCardClass="bg-background/40 rounded-lg border shadow-sm"
	searchIconClass="size-4"
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
