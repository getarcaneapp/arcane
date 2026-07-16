<script lang="ts">
	import { createMutation, createQuery } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { m } from '$lib/paraglide/messages';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { queryKeys } from '$lib/query/query-keys';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import WorkloadUpdatesTable from './workload-updates-table.svelte';
	import { imageService } from '$lib/services/image-service';
	import { containerService, type ContainerListRequestOptions } from '$lib/services/container-service';
	import { projectService } from '$lib/services/project-service';
	import type { ContainersPaginatedResponse } from '$lib/services/container-service';
	import type { ImageUpdateInfoDto } from '$lib/types/docker';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
	import type { Project } from '$lib/types/swarm';
	import { ContainersIcon, ProjectsIcon, UpdateIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import { ensureStandaloneContainerUpdatesFilter, ensureUpdatesFilter } from '$lib/utils/docker';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { hasPermission } from '$lib/utils/auth';
	import type { AutoUpdateCheck } from '$lib/types/automation';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';

	let { data } = $props();

	const initialContainers = untrack(() => data.containers as ContainersPaginatedResponse);
	const initialProjects = untrack(() => data.projects as Paginated<Project>);
	const emptyContainers = untrack(
		() =>
			({
				...initialContainers,
				data: [],
				counts: undefined,
				groups: [],
				pagination: {
					...initialContainers.pagination,
					totalItems: 0,
					totalPages: 0,
					currentPage: 1
				}
			}) satisfies ContainersPaginatedResponse
	);
	const emptyProjects = untrack(
		() =>
			({
				...initialProjects,
				data: [],
				counts: undefined,
				pagination: {
					...initialProjects.pagination,
					totalItems: 0,
					totalPages: 0,
					currentPage: 1
				}
			}) satisfies Paginated<Project>
	);

	let containerSnapshot = $state<{ envId: string; value: ContainersPaginatedResponse } | null>(null);
	let projectSnapshot = $state<{ envId: string; value: Paginated<Project> } | null>(null);
	let requestOptions = $state(untrack(() => data.requestOptions as SearchPaginationSortRequest));
	const envId = $derived(environmentStore.selected?.id || '0');
	const sourceRequestOptions = $derived.by(() => {
		const sourceLimit = (requestOptions.pagination?.page ?? 1) * (requestOptions.pagination?.limit ?? 20);
		return {
			...requestOptions,
			pagination: { page: 1, limit: sourceLimit }
		};
	});
	const containerRequestOptions = $derived(
		ensureStandaloneContainerUpdatesFilter(sourceRequestOptions) as ContainerListRequestOptions
	);
	const projectRequestOptions = $derived(ensureUpdatesFilter(sourceRequestOptions));

	const containersQuery = createQuery(() => ({
		queryKey: queryKeys.containers.list(envId, containerRequestOptions),
		queryFn: () => containerService.getContainersForEnvironment(envId, containerRequestOptions),
		initialData: envId === data.envId ? data.containers : undefined,
		refetchOnMount: false
	}));

	const projectsQuery = createQuery(() => ({
		queryKey: queryKeys.projects.list(envId, projectRequestOptions),
		queryFn: () => projectService.getProjectsForEnvironment(envId, projectRequestOptions),
		initialData: envId === data.envId ? data.projects : undefined,
		refetchOnMount: false
	}));

	const containers = $derived(
		(containerSnapshot?.envId === envId ? containerSnapshot.value : null) ??
			containersQuery.data ??
			(envId === data.envId ? initialContainers : emptyContainers)
	);
	const projects = $derived(
		(projectSnapshot?.envId === envId ? projectSnapshot.value : null) ??
			projectsQuery.data ??
			(envId === data.envId ? initialProjects : emptyProjects)
	);

	const projectUpdatedImageRefs = $derived.by(() => {
		const refs: string[] = [];
		for (const project of projects.data ?? []) {
			for (const imageRef of project.updateInfo?.updatedImageRefs ?? []) {
				if (!refs.includes(imageRef)) refs.push(imageRef);
			}
		}
		return refs.sort();
	});

	const projectUpdateDetailsQuery = createQuery<Record<string, ImageUpdateInfoDto>>(() => ({
		queryKey: ['updates', 'projects', 'details', envId, projectUpdatedImageRefs],
		queryFn: () => imageService.getUpdateInfoByRefs(projectUpdatedImageRefs, envId),
		placeholderData: {},
		enabled: projectUpdatedImageRefs.length > 0
	}));

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: ['updates', 'check-all', envId],
		mutationFn: () => imageService.checkAllImages(envId),
		onSuccess: async () => {
			toast.success(m.images_update_check_completed());
			await Promise.all([containersQuery.refetch(), projectsQuery.refetch()]);
			if (projectUpdatedImageRefs.length > 0) {
				await projectUpdateDetailsQuery.refetch();
			}
		},
		onError: () => {
			toast.error(m.images_update_check_failed());
		}
	}));

	const applyUpdatesMutation = createMutation(() => ({
		mutationKey: ['updates', 'apply', envId],
		mutationFn: (options: AutoUpdateCheck | undefined) => imageService.runAutoUpdate(options, envId),
		onSuccess: async (result) => {
			const toastOptions = activityToastOptions(extractActivityId(result));
			if (result.failed > 0) {
				toast.error(m.operations_update_partial({ updated: result.updated, failed: result.failed }), toastOptions);
			} else {
				toast.success(m.operations_update_complete({ count: result.updated }), toastOptions);
			}
			await refresh();
		},
		onError: () => toast.error(m.operations_update_failed())
	}));

	const isRefreshing = $derived(
		(containersQuery.isFetching && !containersQuery.isPending) || (projectsQuery.isFetching && !projectsQuery.isPending)
	);
	const isChecking = $derived(checkUpdatesMutation.isPending);
	const canApplyUpdates = $derived(hasPermission('image-updates:check', envId));
	const containerCount = $derived(containers.pagination?.totalItems ?? 0);
	const projectCount = $derived(projects.pagination?.totalItems ?? 0);
	const totalAffectedResources = $derived(containerCount + projectCount);

	async function refresh() {
		containerSnapshot = null;
		projectSnapshot = null;
		await Promise.all([containersQuery.refetch(), projectsQuery.refetch()]);
		if (projectUpdatedImageRefs.length > 0) {
			await projectUpdateDetailsQuery.refetch();
		}
	}

	function confirmUpdateAll() {
		openConfirmDialog({
			title: m.operations_update_all_title(),
			message: m.operations_update_all_description(),
			confirm: {
				label: m.operations_update_all(),
				action: async () => {
					await applyUpdatesMutation.mutateAsync(undefined);
				}
			}
		});
	}

	async function updateWorkload(type: 'container' | 'project', resourceId: string) {
		await applyUpdatesMutation.mutateAsync({ type, resourceIds: [resourceId] });
	}

	async function refreshTable(options: SearchPaginationSortRequest) {
		requestOptions = options;
		const sourceLimit = (options.pagination?.page ?? 1) * (options.pagination?.limit ?? 20);
		const sourceOptions = {
			...options,
			pagination: { page: 1, limit: sourceLimit }
		};
		const [nextContainers, nextProjects] = await Promise.all([
			containerService.getContainersForEnvironment(
				envId,
				ensureStandaloneContainerUpdatesFilter(sourceOptions) as ContainerListRequestOptions
			),
			projectService.getProjectsForEnvironment(envId, ensureUpdatesFilter(sourceOptions))
		]);
		containerSnapshot = { envId, value: nextContainers };
		projectSnapshot = { envId, value: nextProjects };
		return { containers: nextContainers, projects: nextProjects };
	}

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'check-updates',
			action: 'inspect',
			label: m.images_check_updates(),
			loadingLabel: m.common_action_checking(),
			onclick: () => checkUpdatesMutation.mutate(),
			loading: isChecking,
			disabled: isChecking
		},
		...(canApplyUpdates
			? [
					{
						id: 'update-all',
						action: 'update' as const,
						label: m.operations_update_all(),
						onclick: confirmUpdateAll,
						loading: applyUpdatesMutation.isPending,
						disabled: applyUpdatesMutation.isPending || totalAffectedResources === 0
					}
				]
			: []),
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: isRefreshing
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.common_total(),
			value: totalAffectedResources,
			icon: UpdateIcon,
			iconColor: 'text-blue-500'
		},
		{
			title: m.containers_title(),
			value: containerCount,
			icon: ContainersIcon,
			iconColor: 'text-emerald-500'
		},
		{
			title: m.projects_title(),
			value: projects.pagination?.totalItems ?? 0,
			icon: ProjectsIcon,
			iconColor: 'text-amber-500'
		}
	]);
</script>

<ResourcePageLayout
	title={m.operations_workload_updates()}
	subtitle={m.operations_updates_subtitle()}
	icon={UpdateIcon}
	{actionButtons}
	{statCards}
>
	{#snippet mainContent()}
		{#key envId}
			<WorkloadUpdatesTable
				{containers}
				{projects}
				environmentName={environmentStore.selected?.name ?? envId}
				onUpdate={updateWorkload}
				bind:requestOptions
				updateInfoByRef={projectUpdateDetailsQuery.data}
				onRefreshData={refreshTable}
			/>
		{/key}
	{/snippet}
</ResourcePageLayout>
