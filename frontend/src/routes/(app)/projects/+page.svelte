<script lang="ts">
	import { browser } from '$app/environment';
	import { AlertIcon, BoxIcon, LayersIcon, ProjectsIcon, StartIcon, StopIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import { resolve } from '$app/paths';
	import ProjectsTable from './projects-table.svelte';
	import SwarmProjectsTable from './swarm-projects-table.svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { m } from '$lib/paraglide/messages';
	import { projectService } from '$lib/services/project-service';
	import { imageService } from '$lib/services/image-service';
	import { swarmService } from '$lib/services/swarm-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { queryKeys } from '$lib/query/query-keys';
	import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { ProjectStatusCounts } from '$lib/types/project.type';
	import type { Paginated } from '$lib/types/pagination.type';
	import type { SwarmStackProjectCounts, SwarmStackProjectSummary } from '$lib/types/swarm.type';
	import { untrack } from 'svelte';
	import { createMutation, createQuery } from '@tanstack/svelte-query';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { PersistedState } from 'runed';
	import { TabBar, type TabItem } from '$lib/components/tab-bar';
	import * as Tabs from '$lib/components/ui/tabs';

	let { data } = $props();
	const emptySwarmProjects: Paginated<SwarmStackProjectSummary> = {
		data: [],
		pagination: {
			totalPages: 1,
			totalItems: 0,
			currentPage: 1,
			itemsPerPage: 20
		}
	};

	function withArchivedFilter(options: SearchPaginationSortRequest, show: boolean): SearchPaginationSortRequest {
		const filters = { ...(options.filters ?? {}) };
		if (show) {
			filters['archived'] = 'true';
		} else {
			delete filters['archived'];
		}

		return {
			...options,
			filters: Object.keys(filters).length > 0 ? filters : undefined
		};
	}

	let baseProjectRequestOptions = $state(untrack(() => withArchivedFilter(data.projectRequestOptions, data.showArchived)));
	let swarmRequestOptions = $state(untrack(() => data.swarmRequestOptions));
	let swarmProjects = $state<Paginated<SwarmStackProjectSummary>>(emptySwarmProjects);
	let selectedIds = $state<string[]>([]);
	let activeTab = $state<'compose' | 'swarm'>('compose');
	let projectsTabState = $state<PersistedState<'compose' | 'swarm'> | null>(null);
	let hasVisitedSwarmTab = $state(false);
	const envId = $derived(environmentStore.selected?.id || '0');
	const showArchived = $derived(page.url.searchParams.get('archived') === 'true');
	const projectRequestOptions = $derived(withArchivedFilter(baseProjectRequestOptions, showArchived));
	const countsFallback: ProjectStatusCounts = {
		runningProjects: 0,
		stoppedProjects: 0,
		totalProjects: 0,
		archivedProjects: 0
	};
	const swarmCountsFallback: SwarmStackProjectCounts = {
		totalStackProjects: 0,
		liveStackProjects: 0,
		downStackProjects: 0,
		unavailableStackProjects: 0
	};

	const projectsQuery = createQuery(() => ({
		queryKey: queryKeys.projects.list(envId, projectRequestOptions),
		queryFn: () => projectService.getProjectsForEnvironment(envId, projectRequestOptions),
		initialData: data.projects,
		refetchOnMount: false
	}));
	let projects = $derived(projectsQuery.data ?? untrack(() => data.projects));

	const projectStatusCountsQuery = createQuery(() => ({
		queryKey: queryKeys.projects.statusCounts(envId),
		queryFn: () => projectService.getProjectStatusCountsForEnvironment(envId),
		initialData: data.projectStatusCounts,
		refetchOnMount: false
	}));
	const swarmProjectsQuery = createQuery(() => ({
		queryKey: queryKeys.swarm.stackProjectsList(envId, swarmRequestOptions),
		queryFn: () => swarmService.getStackProjects(swarmRequestOptions),
		enabled: hasVisitedSwarmTab
	}));
	const swarmProjectCountsQuery = createQuery(() => ({
		queryKey: queryKeys.swarm.stackProjectCounts(envId),
		queryFn: () => swarmService.getStackProjectCounts(),
		enabled: hasVisitedSwarmTab
	}));

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: queryKeys.projects.checkUpdates(envId),
		mutationFn: async () => {
			// Refresh update info for all images, then use the image->project usage
			// map to narrow the redeploy to projects that actually have updates.
			// This avoids hitting every project (and its registry) when nothing has
			// changed, which is especially expensive on instances with many projects.
			await imageService.checkAllImages();

			const images = await imageService.getImagesForEnvironment(envId, { pagination: { page: 1, limit: 10000 } });
			const projectIdsWithUpdates = new Set<string>();
			for (const img of images.data) {
				if (!img.updateInfo?.hasUpdate) continue;
				for (const user of img.usedBy ?? []) {
					if (user.type === 'project' && user.id) {
						projectIdsWithUpdates.add(user.id);
					}
				}
			}

			if (projectIdsWithUpdates.size === 0) {
				return { updated: 0 };
			}

			const allProjects = await projectService.getProjectsForEnvironment(envId, { pagination: { page: 1, limit: 1000 } });
			const projectsToUpdate = allProjects.data.filter((p) => projectIdsWithUpdates.has(p.id));

			const results = await Promise.allSettled(
				projectsToUpdate.map(async (proj) => {
					// deployProject with pullPolicy 'always' already pulls fresh images,
					// so no separate pullProjectImages call is needed.
					await projectService.deployProject(proj.id, { pullPolicy: 'always' });
					return proj.name;
				})
			);
			const failed = results.filter((r): r is PromiseRejectedResult => r.status === 'rejected');
			if (failed.length > 0) {
				const succeeded = results.length - failed.length;
				throw new Error(`${failed.length} project(s) failed to update (${succeeded} succeeded)`);
			}

			return { updated: results.length };
		},
		onSuccess: async (result) => {
			if (result && result.updated === 0) {
				toast.success(m.image_update_up_to_date_title());
			} else {
				toast.success(m.compose_update_success());
			}
			await Promise.all([projectsQuery.refetch(), projectStatusCountsQuery.refetch()]);
		},
		onError: (error) => {
			toast.error(error instanceof Error ? error.message : m.containers_check_updates_failed());
			void Promise.all([projectsQuery.refetch(), projectStatusCountsQuery.refetch()]);
		}
	}));

	$effect(() => {
		if (!browser || projectsTabState) return;

		projectsTabState = new PersistedState<'compose' | 'swarm'>('arcane-projects-tab', 'compose', {
			storage: 'session',
			syncTabs: false
		});

		activeTab = projectsTabState.current ?? 'compose';
		if (activeTab === 'swarm') {
			hasVisitedSwarmTab = true;
		}
	});
	$effect(() => {
		if (swarmProjectsQuery.data) {
			swarmProjects = swarmProjectsQuery.data;
		}
	});
	$effect(() => {
		if (activeTab === 'swarm') {
			hasVisitedSwarmTab = true;
		}
		if (projectsTabState) {
			projectsTabState.current = activeTab;
		}
	});

	const projectStatusCounts = $derived(projectStatusCountsQuery.data ?? countsFallback);
	const swarmProjectCounts = $derived(swarmProjectCountsQuery.data ?? swarmCountsFallback);
	const totalCompose = $derived(projectStatusCounts.totalProjects);
	const runningCompose = $derived(projectStatusCounts.runningProjects);
	const stoppedCompose = $derived(projectStatusCounts.stoppedProjects);
	const archivedCompose = $derived(projectStatusCounts.archivedProjects);
	const totalSwarmProjects = $derived(swarmProjectCounts.totalStackProjects);
	const liveSwarmProjects = $derived(swarmProjectCounts.liveStackProjects);
	const downSwarmProjects = $derived(swarmProjectCounts.downStackProjects);
	const unavailableSwarmProjects = $derived(swarmProjectCounts.unavailableStackProjects);
	let isManualRefreshing = $state(false);
	const isProjectsQueryRefreshing = $derived(projectsQuery.isFetching && !projectsQuery.isPending);
	const isStatusCountsQueryRefreshing = $derived(projectStatusCountsQuery.isFetching && !projectStatusCountsQuery.isPending);
	const isSwarmProjectsQueryRefreshing = $derived(swarmProjectsQuery.isFetching && !swarmProjectsQuery.isPending);
	const isSwarmCountsQueryRefreshing = $derived(swarmProjectCountsQuery.isFetching && !swarmProjectCountsQuery.isPending);
	const isQueryRefreshing = $derived(
		activeTab === 'compose'
			? isProjectsQueryRefreshing || isStatusCountsQueryRefreshing
			: isSwarmProjectsQueryRefreshing || isSwarmCountsQueryRefreshing
	);
	const isRefreshBlocked = $derived(isManualRefreshing || isQueryRefreshing);
	const tabItems = $derived<TabItem[]>([
		{
			value: 'compose',
			label: m.projects_compose_tab(),
			icon: ProjectsIcon
		},
		{
			value: 'swarm',
			label: m.projects_swarm_tab(),
			icon: LayersIcon
		}
	]);

	async function handleCheckForUpdates() {
		await checkUpdatesMutation.mutateAsync();
	}

	function handleTabChange(value: string) {
		activeTab = value as 'compose' | 'swarm';
	}

	async function refreshCompose() {
		if (isRefreshBlocked) return;
		isManualRefreshing = true;
		try {
			await Promise.all([projectsQuery.refetch(), projectStatusCountsQuery.refetch()]);
		} finally {
			isManualRefreshing = false;
		}
	}
	async function refreshSwarm() {
		if (isRefreshBlocked) return;
		isManualRefreshing = true;
		hasVisitedSwarmTab = true;
		try {
			await Promise.all([swarmProjectsQuery.refetch(), swarmProjectCountsQuery.refetch()]);
		} finally {
			isManualRefreshing = false;
		}
	}

	async function toggleArchived(next: boolean) {
		const url = new URL(page.url);
		if (next) {
			url.searchParams.set('archived', 'true');
		} else {
			url.searchParams.delete('archived');
		}
		await goto(`${url.pathname}${url.search}`, { keepFocus: true, replaceState: true, noScroll: true });
	}

	const composeActionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.compose_create_project(),
			onclick: () => goto(resolve('/projects/new'))
		},
		{
			id: 'check-updates',
			action: 'update',
			label: m.compose_update_projects(),
			onclick: handleCheckForUpdates,
			loading: checkUpdatesMutation.isPending,
			disabled: checkUpdatesMutation.isPending
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refreshCompose,
			loading: isManualRefreshing,
			disabled: isRefreshBlocked
		}
	]);
	const swarmActionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.swarm_stack() }),
			onclick: () => goto(resolve('/projects/swarm/new'))
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refreshSwarm,
			loading: isManualRefreshing,
			disabled: isRefreshBlocked
		}
	]);

	const composeStatCards: StatCardConfig[] = $derived([
		{
			title: m.compose_total(),
			value: totalCompose,
			icon: ProjectsIcon,
			iconColor: 'text-amber-500'
		},
		{
			title: m.common_running(),
			value: runningCompose,
			icon: StartIcon,
			iconColor: 'text-green-500'
		},
		{
			title: m.common_stopped(),
			value: stoppedCompose,
			icon: StopIcon,
			iconColor: 'text-red-500'
		},
		{
			title: m.projects_archived_count(),
			value: archivedCompose,
			icon: BoxIcon,
			iconColor: 'text-muted-foreground'
		}
	]);
	const swarmStatCards: StatCardConfig[] = $derived([
		{
			title: m.projects_swarm_saved(),
			value: totalSwarmProjects,
			icon: LayersIcon,
			iconColor: 'text-blue-500'
		},
		{
			title: m.common_running(),
			value: liveSwarmProjects,
			icon: StartIcon,
			iconColor: 'text-green-500'
		},
		{
			title: m.common_stopped(),
			value: downSwarmProjects,
			icon: StopIcon,
			iconColor: 'text-red-500'
		},
		{
			title: m.common_unknown(),
			value: unavailableSwarmProjects,
			icon: AlertIcon,
			iconColor: 'text-amber-500'
		}
	]);
	const actionButtons: ActionButton[] = $derived(activeTab === 'compose' ? composeActionButtons : swarmActionButtons);
	const statCards: StatCardConfig[] = $derived(activeTab === 'compose' ? composeStatCards : swarmStatCards);
	const subtitle = $derived(activeTab === 'compose' ? m.compose_subtitle() : m.projects_swarm_subtitle());
</script>

<ResourcePageLayout title={m.projects_title()} {subtitle} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<Tabs.Root bind:value={activeTab} class="space-y-6">
			<div class="w-fit">
				<TabBar items={tabItems} value={activeTab} onValueChange={handleTabChange} />
			</div>

			<Tabs.Content value="compose">
				<ProjectsTable
					{projects}
					bind:selectedIds
					requestOptions={projectRequestOptions}
					{showArchived}
					onToggleArchived={toggleArchived}
					onRefreshData={async (options) => {
						baseProjectRequestOptions = withArchivedFilter(options, showArchived);
						await Promise.all([projectsQuery.refetch(), projectStatusCountsQuery.refetch()]);
					}}
				/>
			</Tabs.Content>

			<Tabs.Content value="swarm">
				<SwarmProjectsTable
					bind:stackProjects={swarmProjects}
					bind:requestOptions={swarmRequestOptions}
					onRefreshData={async (options) => {
						hasVisitedSwarmTab = true;
						swarmRequestOptions = options;
						const [stackProjectsResult] = await Promise.all([swarmProjectsQuery.refetch(), swarmProjectCountsQuery.refetch()]);
						if (stackProjectsResult.data) {
							swarmProjects = stackProjectsResult.data;
						}
						return swarmProjects;
					}}
				/>
			</Tabs.Content>
		</Tabs.Root>
	{/snippet}
</ResourcePageLayout>
