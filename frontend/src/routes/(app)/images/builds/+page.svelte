<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod/v4';

	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import { ClockIcon, FolderOpenIcon, HammerIcon } from '#lib/icons/index.js';
	import TabbedPageLayout from '#lib/layouts/tabbed-page-layout.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { imageBuildListQuery } from '#lib/query/images.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { isSessionCancelledError } from '#lib/services/api-service.js';
	import { buildWorkspaceService } from '#lib/services/build-workspace-service.js';
	import { containerRegistryService } from '#lib/services/container-registry-service.js';
	import { imageService } from '#lib/services/image-service.js';
	import { settingsService } from '#lib/services/settings-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { ImageBuildRecord, ImageBuildRequest } from '#lib/types/image.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import type { ResolvedSettings } from '#lib/types/ui/settings.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { isDepotBuildAvailable } from '#lib/utils/build-provider.js';
	import { destinationEnvironmentId, readableEnvironments } from '#lib/utils/environments.js';
	import { buildImageReference, getRegistryDisplayName } from '#lib/utils/registry.js';
	import { createForm } from '#lib/utils/settings.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import { createBuildRequest } from './build-request';
	import { BuildRun } from './build-run.svelte';
	import { BuildWorkspaceDraft } from './build-workspace-draft.svelte';
	import BuildConfigPanel from './components/build-config-panel.svelte';
	import BuildControls from './components/build-controls.svelte';
	import type { BuildProviderOption, SelectOption } from './components/build-form.types';
	import BuildOutputPanel from './components/build-output-panel.svelte';
	import BuildSourceSection from './components/build-source-section.svelte';
	import BuildWorkspaceEditor from './components/build-workspace-editor.svelte';
	import ImageBuildHistoryPanel from './components/image-build-history-panel.svelte';
	import {
		formatBuildArgs,
		formatKeyValueMap,
		formatStringList,
		getContextPathFromBuild,
		isGitBuildContextSource
	} from './image-build-history';

	const buildSettingsOptions = (environmentId: string) => ({
		queryKey: queryKeys.settings.byEnvironment(environmentId),
		queryFn: () => settingsService.getSettingsForEnvironmentMerged(environmentId)
	});
	const buildsRootOf = (settings: ResolvedSettings | undefined) =>
		(settings?.values.buildsDirectory ?? '/builds').trim() || '/builds';
	const historyRequestOptions = {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'createdAt', direction: 'desc' }
	} satisfies SearchPaginationSortRequest;
	const registryRequestOptions = {
		pagination: { page: 1, limit: 100 },
		sort: { column: 'url', direction: 'asc' }
	} satisfies SearchPaginationSortRequest;

	let pickedEnvironmentId = $state('');
	const buildEnvironmentId = $derived(destinationEnvironmentId(pickedEnvironmentId, 'images:build'));
	const queryClient = useQueryClient();
	const buildSettingsQuery = createQuery(() => buildSettingsOptions(buildEnvironmentId));
	const buildsRoot = $derived(buildsRootOf(buildSettingsQuery.data));
	const buildsRootLabel = $derived.by(() => {
		if (buildsRoot.length <= 36) return buildsRoot;
		const parts = buildsRoot.split('/').filter(Boolean);
		return parts.length === 0 ? buildsRoot : `…/${parts.slice(-2).join('/')}`;
	});
	const depotAvailable = $derived(isDepotBuildAvailable(buildSettingsQuery.data));
	const maxFileSizeMb = $derived(buildSettingsQuery.data?.values.projectWorkspaceMaxFileSizeMb ?? 10);
	const canEditWorkspace = $derived(hasPermission('images:build', buildEnvironmentId));

	const providerOptions = $derived<BuildProviderOption[]>([
		{ label: m.local_docker(), value: 'local', description: m.local_docker_description() },
		...(depotAvailable ? [{ label: m.depot(), value: 'depot' as const, description: m.depot_description() }] : [])
	]);

	const workspaceQuery = createQuery(() => ({
		queryKey: queryKeys.buildWorkspace.workspace(buildEnvironmentId),
		queryFn: () => buildWorkspaceService.getWorkspace(buildEnvironmentId),
		refetchOnMount: false
	}));
	const workspace = $derived(
		new BuildWorkspaceDraft({
			environmentId: buildEnvironmentId,
			workspace: () => workspaceQuery.data,
			onWorkspace: (next) => queryClient.setQueryData(queryKeys.buildWorkspace.workspace(buildEnvironmentId), next)
		})
	);
	const run = new BuildRun();

	let contextMode = $state<'workspace' | 'remote'>('workspace');
	let remoteContextSource = $state('');
	let showBuildOptions = $state(false);
	let showRuntimeOptions = $state(false);

	const contextDir = $derived.by(() => {
		if (contextMode === 'remote') return remoteContextSource.trim();
		const root = buildsRoot.endsWith('/') ? buildsRoot.slice(0, -1) : buildsRoot;
		return workspace.contextPath ? `${root}/${workspace.contextPath}` : root;
	});

	const formSchema = z.object({
		dockerfile: z.string().optional().default(''),
		tags: z.string().optional().default(''),
		registryId: z.string().default(''),
		repositoryName: z.string().default(''),
		pushTag: z.string().default(''),
		target: z.string().optional().default(''),
		buildArgs: z.string().optional().default(''),
		labels: z.string().optional().default(''),
		cacheFrom: z.string().optional().default(''),
		cacheTo: z.string().optional().default(''),
		network: z.string().optional().default(''),
		isolation: z.string().optional().default(''),
		shmSize: z.string().optional().default(''),
		ulimits: z.string().optional().default(''),
		entitlements: z.string().optional().default(''),
		privileged: z.boolean().default(false),
		extraHosts: z.string().optional().default(''),
		platforms: z.string().optional().default(''),
		noCache: z.boolean().default(false),
		pull: z.boolean().default(false),
		provider: z.enum(['local', 'depot']).default('local'),
		push: z.boolean().default(false),
		load: z.boolean().default(true)
	});

	const form = createForm<typeof formSchema>(formSchema, {
		dockerfile: '',
		tags: '',
		registryId: '',
		repositoryName: '',
		pushTag: '',
		target: '',
		buildArgs: '',
		labels: '',
		cacheFrom: '',
		cacheTo: '',
		network: '',
		isolation: '',
		shmSize: '',
		ulimits: '',
		entitlements: '',
		privileged: false,
		extraHosts: '',
		platforms: '',
		noCache: false,
		pull: false,
		provider: (buildSettingsQuery.data?.values.buildProvider as 'local' | 'depot' | undefined) ?? 'local',
		push: false,
		load: true
	});
	let inputs = $derived(form.inputs);

	const resolvedProvider = $derived(depotAvailable ? inputs.provider.value : 'local');
	const isPushMode = $derived(resolvedProvider === 'depot' ? true : inputs.push.value);

	const registriesQuery = createQuery(() => ({
		queryKey: queryKeys.containerRegistries.list(registryRequestOptions),
		enabled: isPushMode,
		staleTime: 0,
		queryFn: () => containerRegistryService.getRegistries(registryRequestOptions)
	}));
	const registries = $derived(registriesQuery.data?.data ?? []);
	const registryOptions = $derived<SelectOption[]>(
		registries
			.filter((registry) => registry.enabled)
			.map((registry) => {
				const displayName = getRegistryDisplayName(registry);
				return {
					label: registry.url,
					value: registry.id,
					description: displayName === registry.url ? undefined : displayName
				};
			})
	);
	const selectedRegistry = $derived(registries.find((registry) => registry.id === inputs.registryId.value));
	const repositoryOptions = $derived<SelectOption[]>(
		(selectedRegistry?.repositoryNames ?? []).map((name) => ({ label: name, value: name }))
	);
	const fullImageReference = $derived(
		isPushMode && selectedRegistry
			? buildImageReference(selectedRegistry.host, selectedRegistry.namespace, inputs.repositoryName.value, inputs.pushTag.value)
			: ''
	);

	const historyCountQuery = createQuery(() =>
		imageBuildListQuery(environmentStore.shownKey, readableEnvironments('images:list'), historyRequestOptions)
	);

	const mainUrlTab = useUrlTab({
		tabs: () => [
			{ value: 'build', label: m.build(), icon: HammerIcon },
			{ value: 'files', label: m.files(), icon: FolderOpenIcon },
			{
				value: 'history',
				label: m.build_history(),
				icon: ClockIcon,
				badge:
					(historyCountQuery.data?.pagination.totalItems ?? -1) >= 0 ? historyCountQuery.data?.pagination.totalItems : undefined
			}
		],
		defaultTab: () => 'build',
		aliases: () => ({ workspace: 'build' })
	});

	const buildMutation = createMutation(() => ({
		mutationKey: queryKeys.images.buildRun(buildEnvironmentId),
		mutationFn: async (request: ImageBuildRequest & { envId: string }) => {
			const { envId, ...payload } = request;
			await imageService.buildImageStream(payload, envId, run.handleLine);
			if (run.error) throw new Error(run.error);
			run.complete();
		},
		onSettled: async (_data, _error, variables) => {
			await Promise.all(
				[variables.envId, environmentStore.shownKey].map((key) =>
					queryClient.invalidateQueries({ queryKey: queryKeys.images.builds(key) })
				)
			);
		}
	}));

	function selectEnvironment(environmentId: string) {
		pickedEnvironmentId = environmentId;
	}

	let latestApply = 0;
	async function applyBuildConfig(build: ImageBuildRecord, environmentId: string) {
		const apply = ++latestApply;
		const remote = isGitBuildContextSource(build.contextDir);
		const settings = remote ? undefined : await tryCatch(queryClient.ensureQueryData(buildSettingsOptions(environmentId)));
		if (apply !== latestApply) return;
		if (settings?.error) {
			toast.error(m.common_load_failed({ resource: m.settings() }), { description: extractApiErrorMessage(settings.error) });
			return;
		}
		selectEnvironment(environmentId);
		for (const key of ['dockerfile', 'target', 'network', 'isolation'] as const) form.setValue(key, build[key] ?? '');
		for (const key of ['privileged', 'noCache', 'pull', 'push'] as const) form.setValue(key, build[key] ?? false);
		form.setValue('tags', build.tags?.join(', ') ?? '');
		form.setValue('platforms', build.platforms?.join(', ') ?? '');
		form.setValue('buildArgs', formatBuildArgs(build.buildArgs));
		form.setValue('labels', formatKeyValueMap(build.labels));
		form.setValue('cacheFrom', formatStringList(build.cacheFrom));
		form.setValue('cacheTo', formatStringList(build.cacheTo));
		form.setValue('shmSize', build.shmSize ? String(build.shmSize) : '');
		form.setValue('ulimits', formatKeyValueMap(build.ulimits));
		form.setValue('entitlements', formatStringList(build.entitlements));
		form.setValue('extraHosts', formatStringList(build.extraHosts));
		form.setValue('provider', (build.provider as 'local' | 'depot') ?? 'local');
		form.setValue('load', build.load ?? true);

		showBuildOptions =
			[build.dockerfile, build.target].some(Boolean) ||
			(build.platforms?.length ?? 0) > 0 ||
			[build.buildArgs, build.labels].some((values) => Object.keys(values ?? {}).length > 0);
		showRuntimeOptions =
			[build.network, build.isolation, build.privileged, build.pull].some(Boolean) ||
			[build.cacheFrom, build.cacheTo, build.entitlements, build.extraHosts].some((values) => (values?.length ?? 0) > 0) ||
			Object.keys(build.ulimits ?? {}).length > 0 ||
			(build.shmSize ?? 0) > 0;

		contextMode = remote ? 'remote' : 'workspace';
		remoteContextSource = remote ? build.contextDir : '';
		workspace.contextPath = remote ? '' : getContextPathFromBuild(build, buildsRootOf(settings?.data ?? undefined));
		mainUrlTab.select('build');
	}

	async function handleSubmit() {
		const data = form.validate();
		if (!data) return;
		if (!buildSettingsQuery.data && buildSettingsQuery.error) {
			toast.error(m.common_load_failed({ resource: m.settings() }), {
				description: extractApiErrorMessage(buildSettingsQuery.error)
			});
			return;
		}
		if (!buildSettingsQuery.data) {
			toast.info(m.common_loading());
			return;
		}
		if (!contextDir) {
			toast.error(contextMode === 'remote' ? m.build_remote_context_required() : m.build_context_required());
			return;
		}

		const built = createBuildRequest(data, {
			contextDir,
			provider: depotAvailable ? data.provider : 'local',
			registries
		});
		if (!built.request) {
			toast.error(built.error);
			return;
		}

		if (contextMode === 'workspace' && workspace.hasChanges && !(await workspace.save())) return;

		run.start(contextDir);
		const result = await tryCatch(buildMutation.mutateAsync({ envId: buildEnvironmentId, ...built.request }));
		run.isBuilding = false;
		if (result.error === null) {
			toast.success(m.build_completed());
			return;
		}
		if (isSessionCancelledError(result.error)) return;
		const message = result.error instanceof Error ? result.error.message : m.build_failed();
		if (!run.error) run.fail(message);
		toast.error(run.error);
	}
</script>

<TabbedPageLayout tabItems={mainUrlTab.items} selectedTab={mainUrlTab.value} onTabChange={mainUrlTab.select}>
	{#snippet headerInfo()}
		<div class="flex flex-col gap-0.5">
			<h1 class="text-sm font-semibold">{m.builds()}</h1>
			<p class="text-xs text-muted-foreground">{m.manual_build_workspace_subtitle()}</p>
		</div>
	{/snippet}

	{#snippet headerActions()}
		<BuildControls
			bind:inputs
			provider={resolvedProvider}
			{providerOptions}
			environmentId={buildEnvironmentId}
			isBuilding={run.isBuilding || workspace.saving}
			onBuild={handleSubmit}
		/>
	{/snippet}

	{#snippet tabContent(tab)}
		{#if tab === 'history'}
			<ImageBuildHistoryPanel onApplyBuild={applyBuildConfig} />
		{:else if tab === 'files'}
			<div class="flex h-screen-inset-15 min-h-128 flex-col">
				<BuildWorkspaceEditor
					{workspace}
					rootLabel={buildsRootLabel}
					loading={workspaceQuery.isPending}
					error={workspaceQuery.error}
					truncated={workspaceQuery.data?.fileTreeTruncated ?? false}
					canEdit={canEditWorkspace}
					{maxFileSizeMb}
					onRetry={() => workspaceQuery.refetch()}
				/>
			</div>
		{:else}
			<div class="grid items-start gap-4 lg:grid-cols-build-form">
				<BuildConfigPanel
					bind:environmentId={() => buildEnvironmentId, selectEnvironment}
					bind:inputs
					provider={resolvedProvider}
					bind:showBuildOptions
					bind:showRuntimeOptions
					{isPushMode}
					{registryOptions}
					{repositoryOptions}
					{fullImageReference}
					registryLoadFailed={Boolean(registriesQuery.error)}
					onSubmit={handleSubmit}
				>
					{#snippet source()}
						<BuildSourceSection
							{workspace}
							{contextDir}
							{buildsRoot}
							loading={workspaceQuery.isPending}
							bind:contextMode
							bind:remoteContext={remoteContextSource}
							onEditFiles={() => mainUrlTab.select('files')}
						/>
					{/snippet}
				</BuildConfigPanel>
				<div class="h-128 lg:sticky lg:top-0 lg:h-screen-inset-15">
					<BuildOutputPanel {run} />
				</div>
			</div>
		{/if}
	{/snippet}
</TabbedPageLayout>
