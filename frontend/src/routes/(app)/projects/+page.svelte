<script lang="ts">
	import { ProjectsIcon, StartIcon, StopIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import ProjectsTable from './projects-table.svelte';
	import { goto } from '$app/navigation';
	import { m } from '$lib/paraglide/messages';
	import { projectService } from '$lib/services/project-service';
	import { imageService } from '$lib/services/image-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { queryKeys } from '$lib/query/query-keys';
	import type { ProjectStatusCounts } from '$lib/types/project.type';
	import { untrack } from 'svelte';
	import { createMutation, createQuery } from '@tanstack/svelte-query';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';

	let { data } = $props();

	let projects = $state(untrack(() => data.projects));
	let projectRequestOptions = $state(untrack(() => data.projectRequestOptions));
	let selectedIds = $state<string[]>([]);
	const envId = $derived(environmentStore.selected?.id || '0');
	const countsFallback: ProjectStatusCounts = {
		runningProjects: 0,
		stoppedProjects: 0,
		totalProjects: 0
	};

	const projectsQuery = createQuery(() => ({
		queryKey: queryKeys.projects.list(envId, projectRequestOptions),
		queryFn: () => projectService.getProjectsForEnvironment(envId, projectRequestOptions),
		initialData: data.projects
	}));

	const projectStatusCountsQuery = createQuery(() => ({
		queryKey: queryKeys.projects.statusCounts(envId),
		queryFn: () => projectService.getProjectStatusCountsForEnvironment(envId),
		initialData: data.projectStatusCounts
	}));

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: ['projects', 'check-updates', envId],
		mutationFn: () => imageService.runAutoUpdate(),
		onSuccess: async () => {
			toast.success(m.compose_update_success());
			await projectsQuery.refetch();
		},
		onError: () => {
			toast.error(m.containers_check_updates_failed());
		}
	}));

	$effect(() => {
		if (projectsQuery.data) {
			projects = projectsQuery.data;
		}
	});

	const projectStatusCounts = $derived(projectStatusCountsQuery.data ?? countsFallback);
	const totalCompose = $derived(projectStatusCounts.totalProjects);
	const runningCompose = $derived(projectStatusCounts.runningProjects);
	const stoppedCompose = $derived(projectStatusCounts.stoppedProjects);
	let isManualRefreshing = $state(false);
	const isProjectsQueryRefreshing = $derived(projectsQuery.isFetching && !projectsQuery.isPending);
	const isStatusCountsQueryRefreshing = $derived(projectStatusCountsQuery.isFetching && !projectStatusCountsQuery.isPending);
	const isQueryRefreshing = $derived(isProjectsQueryRefreshing || isStatusCountsQueryRefreshing);
	const isRefreshBlocked = $derived(isManualRefreshing || isQueryRefreshing);

	async function handleCheckForUpdates() {
		await checkUpdatesMutation.mutateAsync();
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

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.compose_create_project(),
			onclick: () => goto('/projects/new')
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
		}
	]);
</script>

<ResourcePageLayout title={m.projects_title()} subtitle={m.compose_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<ProjectsTable
			bind:projects
			bind:selectedIds
			bind:requestOptions={projectRequestOptions}
			onRefreshData={async (options) => {
				projectRequestOptions = options;
				await Promise.all([projectsQuery.refetch(), projectStatusCountsQuery.refetch()]);
			}}
		/>
	{/snippet}
</ResourcePageLayout>
