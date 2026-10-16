<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { tick, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import { EmptyState } from '#lib/components/states/index.js';
	import { TABLE_PAGE_SIZE_ALL } from '#lib/constants/table-pagination.js';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import { BoxIcon, ProjectsIcon, StartIcon, StopIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { imageService } from '#lib/services/image-service.js';
	import { projectService } from '#lib/services/project-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { ProjectStatusCounts } from '#lib/types/swarm.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import {
		acrossEnvironmentsNote,
		actionEnvironments,
		fetchAcrossEnvironments,
		hasShownPermission,
		listScopes,
		settleAcrossEnvironments,
		sumAcrossEnvironments,
		withFailedEnvironments
	} from '#lib/utils/environments.js';

	import ProjectsTable from './components/projects-table.svelte';
	import { fetchProjectTags } from './projects-table.helpers';

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
	const queryClient = useQueryClient();
	const scope = $derived(environmentStore.shownKey);
	const showArchived = $derived(page.url.searchParams.get('archived') === 'true');
	const projectRequestOptions = $derived(withArchivedFilter(baseProjectRequestOptions, showArchived));
	const countsFallback: ProjectStatusCounts = {
		runningProjects: 0,
		stoppedProjects: 0,
		totalProjects: 0,
		archivedProjects: 0
	};

	// The load already cached the first page under its key; seeding every key with it would hide other pages and filters.
	const projectsQuery = createQuery(() => {
		const queryScope = scope;
		const environments = environmentStore.shown;
		const options = projectRequestOptions;
		return {
			queryKey: queryKeys.projects.list(queryScope, options),
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) =>
					projectService.getProjectsForEnvironment(id, request, config)
				),
			placeholderData: (previous, query) => {
				if (query?.queryKey[1] === queryScope) return previous;
				return undefined;
			},
			select: (value) => ({ envId: queryScope, value }),
			refetchOnMount: false
		};
	});
	let projects = $derived(projectsQuery.data?.envId === scope ? projectsQuery.data.value : null);

	const projectStatusCountsQuery = createQuery(() => {
		const queryScope = scope;
		const environments = environmentStore.shown;
		return {
			queryKey: queryKeys.projects.statusCounts(queryScope),
			queryFn: () =>
				sumAcrossEnvironments(environments, (id, config) => projectService.getProjectStatusCountsForEnvironment(id, config)),
			initialData: data.scope === queryScope ? data.projectStatusCounts : undefined,
			select: (value) => ({ envId: queryScope, value }),
			refetchOnMount: false
		};
	});
	const projectTagsQuery = createQuery(() => {
		const queryScope = scope;
		const environments = environmentStore.shown;
		return {
			queryKey: queryKeys.projects.tags(queryScope),
			queryFn: () => fetchProjectTags(environments),
			initialData: data.scope === queryScope ? data.projectTags : undefined,
			select: (value) => ({ envId: queryScope, value }),
			refetchOnMount: false
		};
	});
	const projectTags = $derived(projectTagsQuery.data?.envId === scope ? projectTagsQuery.data.value.tags : []);
	const relatedFailures = $derived(
		[projectStatusCountsQuery.data, projectTagsQuery.data].flatMap((result) =>
			result?.envId === scope ? result.value.failedEnvironments : []
		)
	);
	const resourcesReady = $derived(projects !== null);
	// Tags change rarely, so a refresh reloads them only after an environment failed to return them.
	const refetchFailedTags = () =>
		projectTagsQuery.isError || projectTagsQuery.data?.value.failedEnvironments.length ? projectTagsQuery.refetch() : undefined;

	useEnvironmentRefresh(() => {
		selectedIds = [];
		isManualRefreshing = false;
	});

	async function updateEnvironmentProjects(requestedEnvId: string) {
		// Refresh update info for all images, then redeploy only the projects the
		// list reports as having updates. This avoids hitting every project (and
		// its registry) when nothing has changed, which is especially expensive on
		// instances with many projects.
		const imageCheckResults = await imageService.checkAllImages(requestedEnvId);

		// The updates filter is evaluated over every candidate in one pass, so a single unbounded request is cheapest.
		const projectsWithUpdates = await projectService.getProjectsForEnvironment(requestedEnvId, {
			filters: { updates: 'has_update' },
			pagination: { page: 1, limit: TABLE_PAGE_SIZE_ALL }
		});
		// Untracked compose rows cannot be deployed by ID.
		const projectsToUpdate = projectsWithUpdates.data.filter((p) => !p.isDiscovered);

		if (projectsToUpdate.length === 0) {
			return { updated: 0, activityId: extractActivityId(imageCheckResults) };
		}

		const results = await Promise.allSettled(
			projectsToUpdate.map(async (proj) => {
				// deployProject with pullPolicy 'always' already pulls fresh images,
				// so no separate pullProjectImages call is needed.
				await projectService.deployProject(proj.id, 'up', { pullPolicy: 'always' }, requestedEnvId);
				return proj.name;
			})
		);
		const failed = results.filter((r): r is PromiseRejectedResult => r.status === 'rejected');
		if (failed.length > 0) {
			const succeeded = results.length - failed.length;
			throw new Error(`${failed.length} project(s) failed to update (${succeeded} succeeded)`);
		}

		return { updated: results.length, activityId: extractActivityId(imageCheckResults) };
	}

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: queryKeys.projects.checkUpdates(scope),
		mutationFn: async (_requestedScope: string) => {
			const results = await settleAcrossEnvironments(
				actionEnvironments('image-updates:check', 'projects:deploy'),
				updateEnvironmentProjects
			);
			return {
				updated: results.reduce((total, result) => total + result.value.updated, 0),
				activityId: results[0]?.value.activityId,
				environmentIds: results.map(({ environment }) => environment.id)
			};
		},
		onSuccess: async (result, requestedScope) => {
			const toastOptions = activityToastOptions(result.activityId);
			if (result && result.updated === 0) {
				toast.success(m.image_update_up_to_date_title(), toastOptions);
			} else {
				toast.success(m.compose_update_success(), toastOptions);
			}
			// The shown list refetches itself; the affected environments' other cached lists are marked stale.
			await Promise.all(
				listScopes(result.environmentIds)
					.filter((listScope) => listScope !== requestedScope)
					.flatMap((listScope) => [
						queryClient.invalidateQueries({ queryKey: ['projects', listScope] }),
						queryClient.invalidateQueries({ queryKey: queryKeys.projects.statusCounts(listScope) })
					])
			);
			if (requestedScope === scope) {
				await Promise.all([projectsQuery.refetch(), projectStatusCountsQuery.refetch()]);
			}
		},
		onError: (error, requestedScope) => {
			toast.error(error instanceof Error ? error.message : m.containers_check_updates_failed());
			if (requestedScope === scope) {
				void Promise.all([projectsQuery.refetch(), projectStatusCountsQuery.refetch()]);
			}
		}
	}));

	const projectStatusCounts = $derived(
		projectStatusCountsQuery.data?.envId === scope ? projectStatusCountsQuery.data.value : countsFallback
	);
	const totalCompose = $derived(projectStatusCounts.totalProjects);
	const runningCompose = $derived(projectStatusCounts.runningProjects);
	const stoppedCompose = $derived(projectStatusCounts.stoppedProjects);
	const archivedCompose = $derived(projectStatusCounts.archivedProjects);
	const isRefreshBlocked = $derived(isManualRefreshing || projectsQuery.isFetching || projectStatusCountsQuery.isFetching);

	function handleCheckForUpdates() {
		openConfirmDialog({
			title: m.compose_update_projects(),
			message:
				`${m.compose_update_projects_confirm_message()} ${acrossEnvironmentsNote(actionEnvironments('image-updates:check', 'projects:deploy'))}`.trim(),
			confirm: {
				label: m.compose_update_projects(),
				destructive: false,
				action: () => checkUpdatesMutation.mutate(scope)
			}
		});
	}

	async function refreshCompose() {
		if (isRefreshBlocked) return;
		const requestedScope = scope;
		isManualRefreshing = true;
		try {
			await Promise.all([projectsQuery.refetch(), projectStatusCountsQuery.refetch(), refetchFailedTags()]);
		} finally {
			if (requestedScope === scope) {
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
		await goto(`${url.pathname}${url.search}`, { reset: false, replace: true });
	}

	const canCreateProject = $derived(hasShownPermission('projects:create'));
	const canDeployProject = $derived(hasShownPermission('projects:deploy'));

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canCreateProject) {
			buttons.push({
				id: 'create',
				action: 'create',
				placement: 'primary',
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
				disabled: !resourcesReady || checkUpdatesMutation.isPending
			});
		}
		buttons.push({
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
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
			iconColor: 'text-warning'
		},
		{
			title: m.common_running(),
			value: runningCompose,
			icon: StartIcon,
			iconColor: 'text-success'
		},
		{
			title: m.common_stopped(),
			value: stoppedCompose,
			icon: StopIcon,
			iconColor: 'text-destructive'
		},
		{
			title: m.projects_archived_count(),
			value: archivedCompose,
			icon: BoxIcon,
			iconColor: 'text-muted-foreground'
		}
	]);
</script>

{#snippet projectsEmptyState()}
	<EmptyState
		variant="plain"
		icon={ProjectsIcon}
		title={m.projects_empty_title()}
		description={m.projects_empty_description()}
		actionLabel={canCreateProject ? m.compose_create_project() : undefined}
		actionHref="/projects/new"
	/>
{/snippet}

<ResourcePageLayout title={m.projects_title()} subtitle={m.compose_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		{#if projects}
			<ProjectsTable
				projects={withFailedEnvironments(projects, relatedFailures)}
				emptyState={projectsEmptyState}
				bind:selectedIds
				requestOptions={projectRequestOptions}
				{showArchived}
				availableTags={projectTags}
				onToggleArchived={toggleArchived}
				onRefreshData={async (options) => {
					const requestedScope = scope;
					baseProjectRequestOptions = withArchivedFilter(options, showArchived);
					// Let the query switch to the new options first; the refetch then joins its fetch.
					await tick();
					await Promise.all([
						projectsQuery.refetch({ cancelRefetch: false }),
						projectStatusCountsQuery.refetch(),
						refetchFailedTags()
					]);
					if (requestedScope !== scope) {
						selectedIds = [];
					}
				}}
			/>
		{/if}
	{/snippet}
</ResourcePageLayout>
