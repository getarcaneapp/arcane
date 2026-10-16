<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import EmptyState from '#lib/components/states/empty-state.svelte';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import { ContainersIcon, ProjectsIcon, UpdateIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { containerService, type ContainerListRequestOptions } from '#lib/services/container-service.js';
	import { imageService } from '#lib/services/image-service.js';
	import { projectService } from '#lib/services/project-service.js';
	import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { ensureStandaloneContainerUpdatesFilter, ensureUpdatesFilter } from '#lib/utils/docker.js';
	import { actionEnvironments, fetchAcrossEnvironments, listScopes, settleAcrossEnvironments } from '#lib/utils/environments.js';
	import { confirmAndApplyAllUpdates } from '#lib/utils/update-actions.js';

	import ContainerUpdatesTable from './components/container-updates-table.svelte';
	import ProjectUpdatesTable from './components/project-updates-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	let containerRequestOptions = $derived(data.containerRequestOptions as ContainerListRequestOptions);
	let projectRequestOptions = $derived(data.projectRequestOptions as SearchPaginationSortRequest);
	const scope = $derived(environmentStore.shownKey);

	// The page loader already populated these keys, so no initialData is needed.
	// Previous rows are kept only within the same environment, and results are
	// tagged with their environment so a switch never shows another's rows.
	const containersQuery = createQuery(() => {
		const queryScope = scope;
		const environments = environmentStore.shown;
		const options = ensureStandaloneContainerUpdatesFilter(containerRequestOptions);
		return {
			queryKey: queryKeys.containers.list(queryScope, options),
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) =>
					containerService.getContainersForEnvironment(id, request, config)
				),
			placeholderData: (previous, query) => {
				if (query?.queryKey[1] === queryScope) return previous;
				return undefined;
			},
			select: (value) => ({ envId: queryScope, value }),
			refetchOnMount: false
		};
	});

	const projectsQuery = createQuery(() => {
		const queryScope = scope;
		const environments = environmentStore.shown;
		const options = ensureUpdatesFilter(projectRequestOptions);
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

	const containers = $derived(containersQuery.data?.envId === scope ? containersQuery.data.value : null);
	const projects = $derived(projectsQuery.data?.envId === scope ? projectsQuery.data.value : null);

	// Table refreshes write into the cache entry for the environment and request
	// they were issued for, so a late response never replaces another
	// environment's active table. Failures reject so callers can report them;
	// the query's error state also renders inline.
	async function refreshContainers(options: ContainerListRequestOptions) {
		const requestedScope = scope;
		const environments = environmentStore.shown;
		const requestedOptions = ensureStandaloneContainerUpdatesFilter(options);
		await queryClient.fetchQuery({
			queryKey: queryKeys.containers.list(requestedScope, requestedOptions),
			queryFn: () =>
				fetchAcrossEnvironments(environments, requestedOptions, (id, request, config) =>
					containerService.getContainersForEnvironment(id, request, config)
				)
		});
	}

	async function refreshProjects(options: SearchPaginationSortRequest) {
		const requestedScope = scope;
		const environments = environmentStore.shown;
		const requestedOptions = ensureUpdatesFilter(options);
		await queryClient.fetchQuery({
			queryKey: queryKeys.projects.list(requestedScope, requestedOptions),
			queryFn: () =>
				fetchAcrossEnvironments(environments, requestedOptions, (id, request, config) =>
					projectService.getProjectsForEnvironment(id, request, config)
				)
		});
	}

	// Ignoring a container changes its `autoUpdateEnabled` on every container
	// response for this environment. The table refetches its own rows afterwards,
	// so other cached lists are only marked stale here.
	async function invalidateContainerQueries() {
		await Promise.all([
			...listScopes(environmentStore.shown.map((env) => env.id)).map((scope) =>
				queryClient.invalidateQueries({ queryKey: queryKeys.containers.listPrefix(scope), refetchType: 'none' })
			),
			...environmentStore.shown.map((env) =>
				queryClient.invalidateQueries({ queryKey: queryKeys.containers.environment(env.id) })
			)
		]);
	}

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: ['updates', 'check-all', scope],
		mutationFn: (environments: EnvironmentRef[]) =>
			settleAcrossEnvironments(environments, (id) => imageService.checkAllImages(id)),
		onSuccess: async (_result, environments) => {
			toast.success(m.images_update_check_completed());
			await Promise.all([
				...listScopes(environments.map((env) => env.id)).flatMap((scope) =>
					[queryKeys.containers, queryKeys.projects, queryKeys.images].map((keys) =>
						queryClient.invalidateQueries({ queryKey: keys.listPrefix(scope) })
					)
				),
				...environments.flatMap((env) => [
					queryClient.invalidateQueries({ queryKey: queryKeys.containers.environment(env.id) }),
					queryClient.invalidateQueries({ queryKey: queryKeys.projects.environment(env.id) }),
					queryClient.invalidateQueries({ queryKey: queryKeys.images.environment(env.id) })
				])
			]);
		},
		onError: (error) => {
			toast.error(m.images_update_check_failed(), { description: extractApiErrorMessage(error) });
		}
	}));

	const isRefreshing = $derived(
		(containersQuery.isFetching && !containersQuery.isPending) || (projectsQuery.isFetching && !projectsQuery.isPending)
	);
	const isChecking = $derived(checkUpdatesMutation.isPending);
	const containerCount = $derived(containers?.pagination?.totalItems ?? 0);
	const projectCount = $derived(projects?.pagination?.totalItems ?? 0);
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
		await Promise.all([containersQuery.refetch(), projectsQuery.refetch()]);
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
			onclick: () => checkUpdatesMutation.mutate(actionEnvironments('image-updates:check')),
			loading: isChecking,
			disabled: isChecking
		},
		{
			id: 'update-all',
			action: 'update',
			placement: 'primary',
			label: m.update_all(),
			loadingLabel: m.common_action_updating(),
			onclick: updateAll,
			loading: isUpdatingAll,
			disabled: isUpdatingAll || totalAffectedResources === 0
		},
		{
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
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
			iconColor: 'text-info'
		},
		{
			title: m.standalone_containers(),
			value: containerCount,
			icon: ContainersIcon,
			iconColor: 'text-success'
		},
		{
			title: m.projects_title(),
			value: projectCount,
			icon: ProjectsIcon,
			iconColor: 'text-warning'
		}
	]);
</script>

{#snippet loadingState()}
	<div class="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground" role="status" aria-live="polite">
		<Spinner tone="muted" />
		<span>{m.common_loading()}</span>
	</div>
{/snippet}

<ResourcePageLayout title={m.updates()} icon={UpdateIcon} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="space-y-6">
			<Tabs.Root value={effectiveTab}>
				<TabBar items={tabItems} value={effectiveTab} onValueChange={handleTabChange} />

				<Tabs.Content value="containers" class="mt-4">
					{#key `${scope}-containers`}
						{#if containersQuery.isError}
							<EmptyState
								icon={ContainersIcon}
								title={m.common_load_failed({ resource: m.standalone_containers() })}
								description={extractApiErrorMessage(containersQuery.error)}
								actionLabel={m.common_retry()}
								onAction={() => containersQuery.refetch()}
							/>
						{:else if containers}
							<ContainerUpdatesTable
								{containers}
								bind:requestOptions={containerRequestOptions}
								onAutoUpdateChanged={invalidateContainerQueries}
								onRefreshData={refreshContainers}
							/>
						{:else}
							{@render loadingState()}
						{/if}
					{/key}
				</Tabs.Content>

				<Tabs.Content value="projects" class="mt-4">
					{#key `${scope}-projects`}
						{#if projectsQuery.isError}
							<EmptyState
								icon={ProjectsIcon}
								title={m.common_load_failed({ resource: m.projects_title() })}
								description={extractApiErrorMessage(projectsQuery.error)}
								actionLabel={m.common_retry()}
								onAction={() => projectsQuery.refetch()}
							/>
						{:else if projects}
							<ProjectUpdatesTable {projects} bind:requestOptions={projectRequestOptions} onRefreshData={refreshProjects} />
						{:else}
							{@render loadingState()}
						{/if}
					{/key}
				</Tabs.Content>
			</Tabs.Root>
		</div>
	{/snippet}
</ResourcePageLayout>
