<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import {
		SettingsIcon,
		UserIcon,
		SecurityIcon,
		LockIcon,
		NotificationsIcon,
		DockerBrandIcon,
		ApiKeyIcon,
		AppearanceIcon,
		JobsIcon,
		CodeIcon,
		GlobeIcon,
		ActivityIcon
	} from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { settingsSearchService } from '$lib/services/settings-search';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import type { SettingsCategory } from '$lib/types/shared';
	import { canReachAccessSurface, canReachAccessSurfaceUrl } from '$lib/utils/access-policy';
	import { getSettingsSubpageUrlsInNavOrder } from '$lib/config/navigation-config';
	import { useCategorySearch } from '$lib/hooks/use-category-search.svelte';
	import { getCategoryIcon, orderCategoriesByNav } from '$lib/utils/category-page';
	import CategoryIndexPage from '$lib/components/category-index-page.svelte';
	import type { NormalizedCategory } from '$lib/components/category-index-page.types';

	let { data }: PageProps = $props();

	let settingsCategories = $state<SettingsCategory[]>([]);
	const user = $derived(data.user);
	const permissionsManifest = $derived(data.permissionsManifest);
	const categorySearch = useCategorySearch<SettingsCategory>({
		search: (query) => settingsSearchService.search(query),
		filter: isAccessibleCategory,
		onError: (error) => console.error('Search failed:', error)
	});

	const iconMap: Record<string, any> = {
		settings: SettingsIcon,
		database: DockerBrandIcon,
		lock: LockIcon,
		shield: SecurityIcon,
		appearance: AppearanceIcon,
		bell: NotificationsIcon,
		user: UserIcon,
		apikey: ApiKeyIcon,
		jobs: JobsIcon,
		code: CodeIcon,
		globe: GlobeIcon,
		activity: ActivityIcon
	};

	onMount(async () => {
		try {
			settingsCategories = orderCategoriesByNav(
				(await settingsSearchService.getCategories()).filter(isAccessibleCategory),
				getSettingsSubpageUrlsInNavOrder()
			);
		} catch (error) {
			console.error('Failed to load categories:', error);
		}
	});

	function navigateToCategory(categoryUrl: string) {
		goto(categoryUrl);
	}

	function isAccessibleCategory(category: SettingsCategory) {
		if (!permissionsManifest?.accessSurfaces?.length) return true;
		if (category.id === 'jobschedule') {
			return canReachAccessSurface(permissionsManifest, 'settings.category.jobschedule', user, environmentStore.selected?.id);
		}
		return canReachAccessSurfaceUrl(permissionsManifest, category.url, user, environmentStore.selected?.id);
	}

	function getCategoryUrl(category: SettingsCategory) {
		if (category.id === 'jobschedule') {
			return `/environments/${environmentStore.selected?.id ?? '0'}?tab=jobs`;
		}
		return category.url;
	}

	function getIconComponent(iconName: string) {
		return getCategoryIcon(iconMap, iconName, SettingsIcon);
	}

	function normalize(category: SettingsCategory): NormalizedCategory {
		return {
			id: category.id,
			title: category.title,
			description: category.description,
			icon: getIconComponent(category.icon),
			href: getCategoryUrl(category),
			matchingItems: category.matchingSettings
		};
	}

	const normalizedCategories = $derived(settingsCategories.map(normalize));
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
	headerIcon={SettingsIcon}
	title={m.settings()}
	subtitle={m.settings_subtitle()}
	searchPlaceholder={m.settings_search_placeholder()}
	clearSearchLabel={m.common_clear_search()}
	searchingLabel={m.searching()}
	noResultsTitle={m.settings_no_results()}
	noResultsDescription={m.settings_no_results_description()}
	matchingItemsLabel={m.settings_matching_settings()}
	goToPageLabel={m.settings_go_to_page()}
	goToPageButtonTone="outline"
	rootClass="space-y-6 pb-5 md:space-y-8 md:pb-5"
	cardClass="hover:border-primary/30 group cursor-pointer transition-colors duration-200"
	resultCardClass="bg-background/40 rounded-lg border"
	searchIconClass=""
	categories={normalizedCategories}
	categorySearch={searchAdapter}
	navigate={navigateToCategory}
>
	{#snippet resultsHeading()}
		{m.settings_search_results({ query: categorySearch.searchQuery, count: categorySearch.searchResults.length })}
	{/snippet}
	{#snippet moreKeywords(count: number)}
		{m.count_more({ count })}
	{/snippet}
</CategoryIndexPage>
