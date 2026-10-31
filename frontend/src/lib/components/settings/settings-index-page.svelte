<script lang="ts">
	import { goto } from '$app/navigation';
	import type { ResolvedPathname } from '$app/types';
	import { onMount } from 'svelte';

	import CategoryIndexPage from '#lib/components/category-index-page.svelte';
	import type { CategoryGroup, NormalizedCategory } from '#lib/components/category-index-page.types.js';
	import { filterByPermissions, navigationItems } from '#lib/config/navigation-config.js';
	import { useCategorySearch } from '#lib/hooks/use-category-search.svelte.js';
	import {
		SettingsIcon,
		UserIcon,
		SecurityIcon,
		LockIcon,
		NotificationsIcon,
		DockerBrandIcon,
		ApiKeyIcon,
		JobsIcon,
		CodeIcon,
		GlobeIcon,
		ActivityIcon,
		RemoteEnvironmentIcon,
		BackupIcon,
		VolumesIcon,
		ClockIcon,
		type IconType
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { settingsSearchService } from '#lib/services/settings-search.js';
	import type { PermissionsManifest, User } from '#lib/types/auth.js';
	import type { SettingsCategory } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { canReachAccessSurfaceUrl, isInAppPath } from '#lib/utils/auth.js';
	import { normalizeCategory } from '#lib/utils/category-page.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	interface IndexGroup {
		id: string;
		title: string;
		urls: string[];
	}

	interface Props {
		rootUrl: ResolvedPathname;
		headerIcon: IconType;
		title: string;
		subtitle: string;
		searchPlaceholder: string;
		groups: IndexGroup[];
		extraCategories?: NormalizedCategory[];
		navOnlyDescriptions?: Record<string, () => string>;
		user: User | null | undefined;
		permissionsManifest: PermissionsManifest | null | undefined;
	}

	let {
		rootUrl,
		headerIcon,
		title,
		subtitle,
		searchPlaceholder,
		groups,
		extraCategories = [],
		navOnlyDescriptions = {},
		user,
		permissionsManifest
	}: Props = $props();

	let settingsCategories = $state<SettingsCategory[]>([]);
	const categorySearch = useCategorySearch<SettingsCategory>({
		search: (query) => settingsSearchService.search(query),
		filter: isAccessibleCategory
	});

	const iconMap: Record<string, IconType> = {
		settings: SettingsIcon,
		database: DockerBrandIcon,
		lock: LockIcon,
		shield: SecurityIcon,
		bell: NotificationsIcon,
		user: UserIcon,
		apikey: ApiKeyIcon,
		jobs: JobsIcon,
		code: CodeIcon,
		globe: GlobeIcon,
		activity: ActivityIcon,
		remoteenvironment: RemoteEnvironmentIcon,
		backup: BackupIcon,
		volumes: VolumesIcon,
		clock: ClockIcon
	};

	const categoryMessages: Record<string, { title: () => string; description: () => string }> = {
		storage: {
			title: m.storage_limits,
			description: m.settings_category_storage_description
		},
		docker: {
			title: m.docker_title,
			description: m.settings_category_docker_description
		},
		timeouts: {
			title: m.timeouts_settings,
			description: m.timeouts_settings_description
		},
		build: {
			title: m.builds,
			description: m.build_settings_page_description
		},
		activity: {
			title: m.activity,
			description: m.activity_settings_description
		},
		authentication: {
			title: m.authentication,
			description: m.authentication_description
		},
		notifications: {
			title: m.notifications_title,
			description: m.settings_category_notifications_description
		},
		users: {
			title: m.users_title,
			description: m.users_subtitle
		},
		apikeys: {
			title: m.api_key_page_title,
			description: m.settings_category_api_keys_description
		},
		webhooks: {
			title: m.webhook_page_title,
			description: m.webhook_page_description
		},
		jobschedule: {
			title: m.automations,
			description: m.settings_category_automations_description
		},
		security: {
			title: m.security,
			description: m.settings_category_security_description
		},
		s3destinations: {
			title: m.s3_destinations_title,
			description: m.s3_destinations_description
		},
		systembackups: {
			title: m.system_backups_title,
			description: m.system_backups_description
		}
	};

	onMount(async () => {
		await handleApiResultWithCallbacks({
			result: await tryCatch(settingsSearchService.getCategories()),
			message: m.common_load_failed({ resource: title }),
			onSuccess: (categories) => {
				settingsCategories = categories.filter(isAccessibleCategory);
			}
		});
	});

	function navigateToCategory(categoryUrl: string) {
		if (isInAppPath(categoryUrl)) goto(categoryUrl);
	}

	function isAccessibleCategory(category: SettingsCategory) {
		if (!category.url.startsWith(`${rootUrl}/`)) return false;
		if (category.id === 'systembackups' && !user?.isGlobalAdmin) return false;
		if (!permissionsManifest?.accessSurfaces?.length) return true;
		return canReachAccessSurfaceUrl(permissionsManifest, category.url, user);
	}

	function normalize(category: SettingsCategory): NormalizedCategory {
		return {
			...normalizeCategory(category, categoryMessages[category.id], iconMap, SettingsIcon),
			matchingItems: category.matchingSettings
		};
	}

	const navOnlyCategories = $derived(
		filterByPermissions(
			navigationItems.adminItems.find((item) => item.url === rootUrl)?.items,
			user ?? null,
			permissionsManifest
		).flatMap((item): NormalizedCategory[] => {
			const description = navOnlyDescriptions[item.url];
			if (!description) return [];
			return [{ id: item.url, title: item.title, description: description(), icon: item.icon, href: item.url }];
		})
	);

	const groupedCategories = $derived.by((): CategoryGroup[] => {
		const categories = [...settingsCategories.map(normalize), ...navOnlyCategories, ...extraCategories];
		return groups
			.map((group) => ({
				id: group.id,
				title: group.title,
				categories: group.urls.flatMap((url) => categories.filter((category) => category.href === url))
			}))
			.filter((group) => group.categories.length > 0);
	});

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
	{headerIcon}
	{title}
	{subtitle}
	{searchPlaceholder}
	clearSearchLabel={m.common_clear_search()}
	searchingLabel={m.searching()}
	searchFailedLabel={m.common_search_failed()}
	noResultsTitle={m.settings_no_results()}
	noResultsDescription={m.settings_no_results_description()}
	matchingItemsLabel={m.settings_matching_settings()}
	goToPageLabel={m.settings_go_to_page()}
	groups={groupedCategories}
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
