<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { hasPermission } from '#lib/utils/auth';
	import { m } from '#lib/paraglide/messages';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import { queryKeys } from '#lib/query/query-keys';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import ContainerUpdatesTable from './container-updates-table.svelte';
	import ProjectUpdatesTable from './project-updates-table.svelte';
	import { imageService } from '#lib/services/image-service';
	import { containerService, type ContainerListRequestOptions } from '#lib/services/container-service';
	import { projectService } from '#lib/services/project-service';
	import { settingsService } from '#lib/services/settings-service';
	import { confirmAndApplyAllUpdates } from '#lib/utils/update-actions';
	import type { ContainersPaginatedResponse } from '#lib/services/container-service';
	import type { ImageUpdateInfoDto } from '#lib/types/docker';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared';
	import type { Project } from '#lib/types/swarm';
	import { ContainersIcon, ProjectsIcon, UpdateIcon } from '#lib/icons';
	import { toast } from 'svelte-sonner';
	import { ensureStandaloneContainerUpdatesFilter, ensureUpdatesFilter } from '#lib/utils/docker';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const emptyContainers: ContainersPaginatedResponse = {
		data: [],
		groups: [],
		pagination: { totalItems: 0, totalPages: 0, currentPage: 1, itemsPerPage: 100 }
	};
	const emptyProjects: Paginated<Project> = {
		data: [],
		pagination: { totalItems: 0, totalPages: 0, currentPage: 1, itemsPerPage: 20 }
	};
	let containerRequestOptions = $derived(data.containerRequestOptions as ContainerListRequestOptions);
	let projectRequestOptions = $derived(data.projectRequestOptions as SearchPaginationSortRequest);
	const envId = $derived(environmentStore.selected?.id || '0');
	const canReadContainers = $derived(!!environmentStore.selected && hasPermission('containers:list', envId));
	const canReadProjects = $derived(!!environmentStore.selected && hasPermission('projects:list', envId));
	const canReadSettings = $derived(!!environmentStore.selected && hasPermission('settings:read', envId));
	const canReadUpdates = $derived(!!environmentStore.selected && hasPermission('image-updates:read', envId));
	const canCheckUpdates = $derived(!!environmentStore.selected && hasPermission('image-updates:check', envId));

	const containersQuery = createQuery(() => ({
		queryKey: queryKeys.containers.list(envId, ensureStandaloneContainerUpdatesFilter(containerRequestOptions)),
		queryFn: () =>
			containerService.getContainersForEnvironment(envId, ensureStandaloneContainerUpdatesFilter(containerRequestOptions)),
		enabled: canReadContainers,
		initialData:
			canReadContainers &&
			envId === data.envId &&
			JSON.stringify(containerRequestOptions) === JSON.stringify(data.containerRequestOptions)
				? data.containers
				: undefined,
		refetchOnMount: false
	}));

	const projectsQuery = createQuery(() => ({
		queryKey: queryKeys.projects.list(envId, ensureUpdatesFilter(projectRequestOptions)),
		queryFn: () => projectService.getProjectsForEnvironment(envId, ensureUpdatesFilter(projectRequestOptions)),
		enabled: canReadProjects,
		initialData:
			canReadProjects &&
			envId === data.envId &&
			JSON.stringify(projectRequestOptions) === JSON.stringify(data.projectRequestOptions)
				? data.projects
				: undefined,
		refetchOnMount: false
	}));

	const containers = $derived(canReadContainers ? (containersQuery.data ?? emptyContainers) : emptyContainers);
	const projects = $derived(canReadProjects ? (projectsQuery.data ?? emptyProjects) : emptyProjects);

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
		queryKey: [...queryKeys.settings.byEnvironment(envId), 'updates'],
		enabled: canReadSettings,
		queryFn: () => settingsService.getSettingsForEnvironment(envId),
		initialData: envId === data.envId ? data.settings : undefined,
		refetchOnMount: false
	}));

	const excludedContainers = $derived((canReadSettings ? settingsQuery.data?.autoUpdateExcludedContainers : undefined) ?? '');

	const projectUpdateDetailsQuery = createQuery<Record<string, ImageUpdateInfoDto>>(() => ({
		queryKey: ['updates', 'projects', 'details', envId, projectUpdatedImageRefs],
		queryFn: () =>
			projectUpdatedImageRefs.length > 0 ? imageService.getUpdateInfoByRefs(projectUpdatedImageRefs, envId) : Promise.resolve({}),
		enabled: canReadProjects && canReadUpdates && projectUpdatedImageRefs.length > 0
	}));

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: ['updates', 'check-all', envId],
		mutationFn: (environmentId: string) => imageService.checkAllImages(environmentId),
		onSuccess: async (_, environmentId) => {
			if (environmentId !== envId) return;
			toast.success(m.images_update_check_completed());
			await Promise.all([
				canReadContainers ? containersQuery.refetch() : undefined,
				canReadProjects ? projectsQuery.refetch() : undefined
			]);
			if (canReadProjects && canReadUpdates && projectUpdatedImageRefs.length > 0) {
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
	const tabItems: TabItem[] = $derived(
		[
			{
				value: 'containers',
				label: m.containers(),
				icon: ContainersIcon
			},
			{
				value: 'projects',
				label: m.projects_title(),
				icon: ProjectsIcon
			}
		].filter((tab) => (tab.value === 'containers' ? canReadContainers : canReadProjects))
	);
	type UpdateTab = 'containers' | 'projects';
	const urlTab = useUrlTab<UpdateTab>({
		validTabs: () => tabItems.map((tab) => tab.value as UpdateTab),
		defaultTab: () =>
			canReadContainers && (containerCount > 0 || !canReadProjects || projectCount === 0) ? 'containers' : 'projects'
	});
	const effectiveTab = $derived(urlTab.value);

	async function refresh() {
		await Promise.all([
			canReadContainers ? containersQuery.refetch() : undefined,
			canReadProjects ? projectsQuery.refetch() : undefined
		]);
		if (canReadProjects && canReadUpdates && projectUpdatedImageRefs.length > 0) {
			await projectUpdateDetailsQuery.refetch();
		}
	}

	// The run is synchronous server-side (bounded by the updater apply timeout),
	// so the button holds its spinner while the Activity Center streams progress.
	let isUpdatingAll = $state(false);

	function updateAll() {
		if (!canCheckUpdates) return;
		confirmAndApplyAllUpdates({
			setLoading: (loading) => (isUpdatingAll = loading),
			onRefresh: refresh
		});
	}

	function handleTabChange(value: string) {
		urlTab.select(value);
	}

	const actionButtons: ActionButton[] = $derived(
		(
			[
				{
					id: 'check-updates',
					action: 'inspect',
					label: m.images_check_updates(),
					loadingLabel: m.common_action_checking(),
					onclick: () => {
						if (canCheckUpdates) checkUpdatesMutation.mutate(envId);
					},
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
			] satisfies ActionButton[]
		).filter((action) => action.id === 'refresh' || canCheckUpdates)
	);

	const statCards: StatCardConfig[] = $derived(
		[
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
		].filter((_, index) => index === 0 || (index === 1 ? canReadContainers : canReadProjects))
	);
</script>

<ResourcePageLayout environmentScoped title={m.updates()} icon={UpdateIcon} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="space-y-6">
			<Tabs.Root value={effectiveTab}>
				<TabBar items={tabItems} value={effectiveTab} onValueChange={handleTabChange} />

				{#if canReadContainers}
					<Tabs.Content value="containers" class="mt-4">
						<p class="mb-4 text-sm text-muted-foreground">{m.updates_containers_description()}</p>
						{#key `${envId}-containers`}
							<ContainerUpdatesTable
								{containers}
								error={containersQuery.isError}
								loading={containersQuery.isPending}
								{excludedContainers}
								bind:requestOptions={containerRequestOptions}
								onIgnoreChanged={async () => {
									if (canReadSettings) await settingsQuery.refetch();
								}}
								onRefreshData={async (options) => {
									const requestEnvId = envId;
									containerRequestOptions = ensureStandaloneContainerUpdatesFilter(options);
									const next = await queryClient.fetchQuery({
										queryKey: queryKeys.containers.list(requestEnvId, containerRequestOptions),
										queryFn: () => containerService.getContainersForEnvironment(requestEnvId, containerRequestOptions)
									});
									return next;
								}}
							/>
						{/key}
					</Tabs.Content>
				{/if}

				{#if canReadProjects}
					<Tabs.Content value="projects" class="mt-4">
						{#key `${envId}-projects`}
							<ProjectUpdatesTable
								{projects}
								error={projectsQuery.isError}
								loading={projectsQuery.isPending}
								bind:requestOptions={projectRequestOptions}
								updateInfoByRef={projectUpdateDetailsQuery.data}
								onRefreshData={async (options) => {
									const requestEnvId = envId;
									projectRequestOptions = ensureUpdatesFilter(options);
									await queryClient.fetchQuery({
										queryKey: queryKeys.projects.list(requestEnvId, projectRequestOptions),
										queryFn: () => projectService.getProjectsForEnvironment(requestEnvId, projectRequestOptions)
									});
								}}
							/>
						{/key}
					</Tabs.Content>
				{/if}
			</Tabs.Root>
		</div>
	{/snippet}
</ResourcePageLayout>
