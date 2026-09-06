<script lang="ts">
	import { createMutation, createQuery, keepPreviousData } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import ContainerUpdatesTable from './container-updates-table.svelte';
	import ProjectUpdatesTable from './project-updates-table.svelte';
	import { imageService } from '#lib/services/image-service.js';
	import { containerService, type ContainerListRequestOptions } from '#lib/services/container-service.js';
	import { projectService } from '#lib/services/project-service.js';
	import { settingsService } from '#lib/services/settings-service.js';
	import { confirmAndApplyAllUpdates } from '#lib/utils/update-actions.js';
	import type { ContainersPaginatedResponse } from '#lib/services/container-service.js';
	import type { ImageUpdateInfoDto } from '#lib/types/docker.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { Project } from '#lib/types/swarm.js';
	import { ContainersIcon, ProjectsIcon, UpdateIcon } from '#lib/icons/index.js';
	import { toast } from 'svelte-sonner';
	import { ensureStandaloneContainerUpdatesFilter, ensureUpdatesFilter } from '#lib/utils/docker.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';

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
	let containerRequestOptions = $derived(data.containerRequestOptions as ContainerListRequestOptions);
	let projectRequestOptions = $derived(data.projectRequestOptions as SearchPaginationSortRequest);
	const envId = $derived(environmentStore.selected?.id || '0');

	const containersQuery = createQuery(() => ({
		queryKey: queryKeys.containers.list(envId, ensureStandaloneContainerUpdatesFilter(containerRequestOptions)),
		queryFn: () =>
			containerService.getContainersForEnvironment(envId, ensureStandaloneContainerUpdatesFilter(containerRequestOptions)),
		placeholderData: keepPreviousData,
		initialData: envId === data.envId ? data.containers : undefined,
		refetchOnMount: false
	}));

	const projectsQuery = createQuery(() => ({
		queryKey: queryKeys.projects.list(envId, ensureUpdatesFilter(projectRequestOptions)),
		queryFn: () => projectService.getProjectsForEnvironment(envId, ensureUpdatesFilter(projectRequestOptions)),
		placeholderData: keepPreviousData,
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
		const refs = new Set<string>();
		for (const project of projects.data ?? []) {
			for (const imageRef of project.updateInfo?.updatedImageRefs ?? []) {
				refs.add(imageRef);
			}
		}
		return Array.from(refs).sort();
	});

	const settingsQuery = createQuery(() => ({
		queryKey: queryKeys.settings.byEnvironment(envId),
		queryFn: () => settingsService.getSettingsForEnvironmentMerged(envId),
		initialData: envId === data.envId ? data.settings : undefined,
		refetchOnMount: false
	}));

	const excludedContainers = $derived(settingsQuery.data?.autoUpdateExcludedContainers ?? '');

	const projectUpdateDetailsQuery = createQuery<Record<string, ImageUpdateInfoDto>>(() => ({
		queryKey: ['updates', 'projects', 'details', envId, projectUpdatedImageRefs],
		queryFn: () =>
			projectUpdatedImageRefs.length > 0 ? imageService.getUpdateInfoByRefs(projectUpdatedImageRefs) : Promise.resolve({}),
		enabled: projectUpdatedImageRefs.length > 0
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
			label: m.standalone_containers(),
			icon: ContainersIcon
		},
		{
			value: 'projects',
			label: m.projects_title(),
			icon: ProjectsIcon
		}
	]);
	type UpdateTab = 'containers' | 'projects';
	const urlTab = useUrlTab<UpdateTab>({
		validTabs: () => {
			if (containerCount === 0 && projectCount > 0) return ['projects'];
			if (projectCount === 0 && containerCount > 0) return ['containers'];
			return ['containers', 'projects'];
		},
		defaultTab: () => (containerCount > 0 || projectCount === 0 ? 'containers' : 'projects')
	});
	const effectiveTab = $derived(urlTab.value);

	async function refresh() {
		containerSnapshot = null;
		projectSnapshot = null;
		await Promise.all([containersQuery.refetch(), projectsQuery.refetch()]);
		if (projectUpdatedImageRefs.length > 0) {
			await projectUpdateDetailsQuery.refetch();
		}
	}

	// The run is synchronous server-side (bounded by the updater apply timeout),
	// so the button holds its spinner while the Activity Center streams progress.
	let isUpdatingAll = $state(false);

	function updateAll() {
		confirmAndApplyAllUpdates({
			setLoading: (loading) => (isUpdatingAll = loading),
			onRefresh: refresh
		});
	}

	function handleTabChange(value: string) {
		urlTab.select(value);
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
			id: 'update-all',
			action: 'update',
			label: m.update_all(),
			loadingLabel: m.common_action_updating(),
			onclick: updateAll,
			loading: isUpdatingAll,
			disabled: isUpdatingAll || totalAffectedResources === 0
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
			title: m.standalone_containers(),
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

<ResourcePageLayout title={m.updates()} icon={UpdateIcon} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="space-y-6">
			<Tabs.Root value={effectiveTab}>
				<TabBar items={tabItems} value={effectiveTab} onValueChange={handleTabChange} />

				<Tabs.Content value="containers" class="mt-4">
					{#key `${envId}-containers`}
						<ContainerUpdatesTable
							{containers}
							{excludedContainers}
							bind:requestOptions={containerRequestOptions}
							onIgnoreChanged={() => settingsQuery.refetch()}
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
							{projects}
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
