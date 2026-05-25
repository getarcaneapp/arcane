<script lang="ts">
	import { BoxIcon, ProjectsIcon, StartIcon, StopIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import ProjectsTable from './projects-table.svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { m } from '$lib/paraglide/messages';
	import { projectService } from '$lib/services/project-service';
	import { imageService } from '$lib/services/image-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import { queryKeys } from '$lib/query/query-keys';
	import type { SearchPaginationSortRequest } from '$lib/types/shared';
	import type { ProjectStatusCounts } from '$lib/types/swarm';
	import { untrack } from 'svelte';
	import { createMutation, createQuery } from '@tanstack/svelte-query';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';

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
	const envId = $derived(environmentStore.selected?.id || '0');
	const showArchived = $derived(page.url.searchParams.get('archived') === 'true');
	const projectRequestOptions = $derived(withArchivedFilter(baseProjectRequestOptions, showArchived));
	const countsFallback: ProjectStatusCounts = {
		runningProjects: 0,
		stoppedProjects: 0,
		totalProjects: 0,
		archivedProjects: 0
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

	const projectStatusCounts = $derived(projectStatusCountsQuery.data ?? countsFallback);
	const totalCompose = $derived(projectStatusCounts.totalProjects);
	const runningCompose = $derived(projectStatusCounts.runningProjects);
	const stoppedCompose = $derived(projectStatusCounts.stoppedProjects);
	const archivedCompose = $derived(projectStatusCounts.archivedProjects);
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

	async function toggleArchived(next: boolean) {
		const url = new URL(page.url);
		if (next) {
			url.searchParams.set('archived', 'true');
		} else {
			url.searchParams.delete('archived');
		}
		await goto(`${url.pathname}${url.search}`, { keepFocus: true, replaceState: true, noScroll: true });
	}

	const canCreateProject = $derived(hasPermission('projects:create', envId));
	const canDeployProject = $derived(hasPermission('projects:deploy', envId));

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
		if (canDeployProject) {
			buttons.push({
				id: 'check-updates',
				action: 'update',
				label: m.compose_update_projects(),
				onclick: handleCheckForUpdates,
				loading: checkUpdatesMutation.isPending,
				disabled: checkUpdatesMutation.isPending
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

<ResourcePageLayout title={m.projects_title()} subtitle={m.compose_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
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
	{/snippet}
</ResourcePageLayout>
