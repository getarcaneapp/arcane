<script lang="ts">
	import { BoxIcon, ProjectsIcon, StartIcon, StopIcon } from '$lib/icons';
	import ProjectsTable from '../../projects/projects-table.svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { m } from '$lib/paraglide/messages';
	import { projectService } from '$lib/services/project-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import { queryKeys } from '$lib/query/query-keys';
	import type { SearchPaginationSortRequest } from '$lib/types/shared';
	import type { ProjectStatusCounts } from '$lib/types/swarm';
	import { untrack } from 'svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import WorkloadTabs from '$lib/components/workloads/workload-tabs.svelte';

	let { data } = $props();

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
	let selectedIds = $state<string[]>([]);
	let isManualRefreshing = $state(false);
	const envId = $derived(environmentStore.selected?.id || '0');
	let previousEnvId = untrack(() => envId);
	const showArchived = $derived(page.url.searchParams.get('archived') === 'true');
	const projectRequestOptions = $derived(withArchivedFilter(baseProjectRequestOptions, showArchived));
	const countsFallback: ProjectStatusCounts = {
		runningProjects: 0,
		stoppedProjects: 0,
		totalProjects: 0,
		archivedProjects: 0
	};

	const projectsQuery = createQuery(() => {
		const queryEnvId = envId;
		return {
			queryKey: queryKeys.projects.list(queryEnvId, projectRequestOptions),
			queryFn: () => projectService.getProjectsForEnvironment(queryEnvId, projectRequestOptions),
			initialData: data.envId === queryEnvId ? data.projects : undefined,
			select: (value) => ({ envId: queryEnvId, value }),
			refetchOnMount: false
		};
	});
	let projects = $derived(projectsQuery.data?.envId === envId ? projectsQuery.data.value : null);

	const projectStatusCountsQuery = createQuery(() => {
		const queryEnvId = envId;
		return {
			queryKey: queryKeys.projects.statusCounts(queryEnvId),
			queryFn: () => projectService.getProjectStatusCountsForEnvironment(queryEnvId),
			initialData: data.envId === queryEnvId ? data.projectStatusCounts : undefined,
			select: (value) => ({ envId: queryEnvId, value }),
			refetchOnMount: false
		};
	});
	const resourcesReady = $derived(projects !== null);

	$effect(() => {
		if (envId === previousEnvId) return;
		previousEnvId = envId;
		selectedIds = [];
		isManualRefreshing = false;
	});

	const projectStatusCounts = $derived(
		projectStatusCountsQuery.data?.envId === envId ? projectStatusCountsQuery.data.value : countsFallback
	);
	const totalCompose = $derived(projectStatusCounts.totalProjects);
	const runningCompose = $derived(projectStatusCounts.runningProjects);
	const stoppedCompose = $derived(projectStatusCounts.stoppedProjects);
	const archivedCompose = $derived(projectStatusCounts.archivedProjects);
	const isRefreshBlocked = $derived(isManualRefreshing || projectsQuery.isFetching || projectStatusCountsQuery.isFetching);

	async function refreshCompose() {
		if (isRefreshBlocked) return;
		const requestedEnvId = envId;
		isManualRefreshing = true;
		try {
			await Promise.all([projectsQuery.refetch(), projectStatusCountsQuery.refetch()]);
		} finally {
			if (requestedEnvId === envId) {
				isManualRefreshing = false;
			}
		}
	}

	async function toggleArchived(next: boolean) {
		const url = new URL(page.url.href);
		if (next) {
			url.searchParams.set('archived', 'true');
		} else {
			url.searchParams.delete('archived');
		}
		await goto(`${url.pathname}${url.search}`, { keepFocus: true, replaceState: true, noScroll: true });
	}

	const canCreateProject = $derived(hasPermission('projects:create', envId));
	const canReviewUpdates = $derived(hasPermission('image-updates:read', envId));

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canCreateProject) {
			buttons.push({
				id: 'create',
				action: 'create',
				label: m.compose_create_project(),
				onclick: () => goto('/projects/new')
			});
		}
		if (canReviewUpdates) {
			buttons.push({
				id: 'review-updates',
				action: 'update',
				label: m.images_updates(),
				onclick: () => goto('/operations/updates?tab=projects'),
				disabled: !resourcesReady
			});
		}
		buttons.push({
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refreshCompose,
			loading: isManualRefreshing,
			disabled: isRefreshBlocked
		});
		return buttons;
	});

	const statCards: StatCardConfig[] = $derived([
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
</script>

<ResourcePageLayout title={m.workloads_title()} subtitle={m.workloads_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="mb-4">
			<WorkloadTabs value="projects" />
		</div>
		{#if projects}
			<ProjectsTable
				{projects}
				bind:selectedIds
				requestOptions={projectRequestOptions}
				{showArchived}
				onToggleArchived={toggleArchived}
				onRefreshData={async (options) => {
					const requestedEnvId = envId;
					baseProjectRequestOptions = withArchivedFilter(options, showArchived);
					await Promise.all([projectsQuery.refetch(), projectStatusCountsQuery.refetch()]);
					if (requestedEnvId !== envId) {
						selectedIds = [];
					}
				}}
			/>
		{/if}
	{/snippet}
</ResourcePageLayout>
