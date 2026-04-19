<script lang="ts">
	import { createMutation, createQuery } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { m } from '$lib/paraglide/messages';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { queryKeys } from '$lib/query/query-keys';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { TabBar, type TabItem } from '$lib/components/tab-bar';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import ContainerUpdatesTable from './container-updates-table.svelte';
	import ProjectUpdatesTable from './project-updates-table.svelte';
	import { imageService } from '$lib/services/image-service';
	import { containerService, type ContainerListRequestOptions } from '$lib/services/container-service';
	import { projectService } from '$lib/services/project-service';
	import type { ContainersPaginatedResponse } from '$lib/services/container-service';
	import type { ImageUpdateInfoDto } from '$lib/types/image.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { Project } from '$lib/types/project.type';
	import { ContainersIcon, ProjectsIcon, UpdateIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import { ensureStandaloneContainerUpdatesFilter, ensureUpdatesFilter } from '$lib/utils/updates-filter.util';

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
	let containerRequestOptions = $state(untrack(() => data.containerRequestOptions as ContainerListRequestOptions));
	let projectRequestOptions = $state(untrack(() => data.projectRequestOptions as SearchPaginationSortRequest));
	let activeTab = $state<'containers' | 'projects'>(
		untrack(() =>
			(data.containers.pagination?.totalItems ?? 0) > 0 || (data.projects.pagination?.totalItems ?? 0) === 0
				? 'containers'
				: 'projects'
		)
	);
	const envId = $derived(environmentStore.selected?.id || '0');
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

	const containersQuery = createQuery(() => ({
		queryKey: queryKeys.containers.list(envId, ensureStandaloneContainerUpdatesFilter(containerRequestOptions)),
		queryFn: () =>
			containerService.getContainersForEnvironment(envId, ensureStandaloneContainerUpdatesFilter(containerRequestOptions)),
		initialData: envId === data.envId ? data.containers : undefined,
		refetchOnMount: false
	}));

	const projectsQuery = createQuery(() => ({
		queryKey: queryKeys.projects.list(envId, ensureUpdatesFilter(projectRequestOptions)),
		queryFn: () => projectService.getProjectsForEnvironment(envId, ensureUpdatesFilter(projectRequestOptions)),
		initialData: envId === data.envId ? data.projects : undefined,
		refetchOnMount: false
	}));

	const projectUpdatedImageRefs = $derived.by(() => {
		const refs = new Set<string>();
		for (const project of projects.data ?? []) {
			for (const imageRef of project.updateInfo?.updatedImageRefs ?? []) {
				refs.add(imageRef);
			}
		}
		return Array.from(refs).sort();
	});

	const projectUpdateDetailsQuery = createQuery<Record<string, ImageUpdateInfoDto>>(() => ({
		queryKey: ['updates', 'projects', 'details', envId, projectUpdatedImageRefs],
		queryFn: () =>
			projectUpdatedImageRefs.length > 0 ? imageService.getUpdateInfoByRefs(projectUpdatedImageRefs) : Promise.resolve({}),
		initialData: {},
		enabled: projectUpdatedImageRefs.length > 0,
		refetchOnMount: false
	}));

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: ['updates', 'check-all', envId],
		mutationFn: () => imageService.checkAllImages(),
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

	const isRefreshing = $derived(
		(containersQuery.isFetching && !containersQuery.isPending) || (projectsQuery.isFetching && !projectsQuery.isPending)
	);
	const isChecking = $derived(checkUpdatesMutation.isPending);
	const containerCount = $derived(containers.pagination?.totalItems ?? 0);
	const projectCount = $derived(projects.pagination?.totalItems ?? 0);
	const totalAffectedResources = $derived(containerCount + projectCount);
	const tabItems: TabItem[] = $derived([
		{
			value: 'containers',
			label: m.containers_title(),
			icon: ContainersIcon
		},
		{
			value: 'projects',
			label: m.projects_title(),
			icon: ProjectsIcon
		}
	]);
	const effectiveTab = $derived(
		activeTab === 'containers' && containerCount === 0 && projectCount > 0
			? 'projects'
			: activeTab === 'projects' && projectCount === 0 && containerCount > 0
				? 'containers'
				: activeTab
	);

	async function refresh() {
		containerSnapshot = null;
		projectSnapshot = null;
		await Promise.all([containersQuery.refetch(), projectsQuery.refetch()]);
		if (projectUpdatedImageRefs.length > 0) {
			await projectUpdateDetailsQuery.refetch();
		}
	}

	function handleTabChange(value: string) {
		activeTab = value === 'projects' ? 'projects' : 'containers';
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

<ResourcePageLayout title={m.images_updates()} icon={UpdateIcon} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="space-y-6">
			<Tabs.Root value={effectiveTab}>
				<TabBar items={tabItems} value={effectiveTab} onValueChange={handleTabChange} />

				<Tabs.Content value="containers" class="mt-4">
					{#key `${envId}-containers`}
						<ContainerUpdatesTable
							bind:containers={() => containers, (value) => (containerSnapshot = { envId, value })}
							bind:requestOptions={containerRequestOptions}
							onRefreshData={async (options) => {
								containerRequestOptions = ensureStandaloneContainerUpdatesFilter(options);
								const next = await containerService.getContainersForEnvironment(envId, containerRequestOptions);
								containerSnapshot = { envId, value: next };
								return next;
							}}
						/>
					{/key}
				</Tabs.Content>

				<Tabs.Content value="projects" class="mt-4">
					{#key `${envId}-projects`}
						<ProjectUpdatesTable
							bind:projects={() => projects, (value) => (projectSnapshot = { envId, value })}
							bind:requestOptions={projectRequestOptions}
							updateInfoByRef={projectUpdateDetailsQuery.data}
							onRefreshData={async (options) => {
								projectRequestOptions = ensureUpdatesFilter(options);
								projectSnapshot = {
									envId,
									value: await projectService.getProjectsForEnvironment(envId, projectRequestOptions)
								};
							}}
						/>
					{/key}
				</Tabs.Content>
			</Tabs.Root>
		</div>
	{/snippet}
</ResourcePageLayout>
