<script lang="ts">
	import { onMount } from 'svelte';
	import { z } from 'zod/v4';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.js';
	import { createForm } from '#lib/utils/settings.js';
	import { isDepotBuildAvailable } from '#lib/utils/build-provider.js';
	import { toast } from 'svelte-sonner';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { ResourceDetailLayout } from '#lib/layouts/index.js';
	import TabbedPageLayout from '#lib/layouts/tabbed-page-layout.svelte';
	import { sanitizeLogText } from '#lib/utils/formatting.js';
	import { CodeIcon, TerminalIcon } from '#lib/icons/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { extractErrorMessage } from '#lib/utils/docker.js';
	import ResizableSplit from '#lib/components/resizable-split.svelte';
	import BuildControls from './components/build-controls.svelte';
	import BuildWorkspacePanel from './components/build-workspace-panel.svelte';
	import BuildConfigPanel from './components/build-config-panel.svelte';
	import BuildOutputPanel from './components/build-output-panel.svelte';
	import ImageBuildHistoryPanel from './components/image-build-history-panel.svelte';
	import type { BuildProviderOption, SelectOption } from './components/build-form.types';
	import { containerRegistryService } from '#lib/services/container-registry-service.js';
	import { buildImageReference, getRegistryDisplayName } from '#lib/utils/registry.js';
	import { parseList } from '#lib/utils/form-parsers.js';
	import type { ImageBuildRecord } from '#lib/types/docker.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import {
		formatBuildArgs,
		formatKeyValueMap,
		formatStringList,
		getContextPathFromBuild,
		isGitBuildContextSource
	} from './image-build-history';

	const buildsRoot = $derived((($settingsStore?.buildsDirectory ?? '/builds') as string).trim() || '/builds');
	const buildsRootLabel = $derived.by(() => {
		const raw = buildsRoot.trim();
		if (!raw) return '/builds';
		if (raw.length <= 36) return raw;
		const parts = raw.split('/').filter(Boolean);
		if (parts.length === 0) return raw;
		const tail = parts.slice(-2).join('/');
		return `…/${tail}`;
	});

	const depotAvailable = $derived(isDepotBuildAvailable($settingsStore));

	const providerOptions = $derived.by<BuildProviderOption[]>(() => {
		const options: BuildProviderOption[] = [
			{ label: m.local_docker(), value: 'local', description: m.local_docker_description() }
		];
		if (depotAvailable) {
			options.push({ label: m.depot(), value: 'depot', description: m.depot_description() });
		}
		return options;
	});

	let selectedContextPath = $state('/');
	let contextMode = $state<'workspace' | 'remote'>('workspace');
	let remoteContextSource = $state('');

	const workspaceContextDir = $derived.by(() => {
		const root = buildsRoot.endsWith('/') ? buildsRoot.slice(0, -1) : buildsRoot;
		if (selectedContextPath === '/' || selectedContextPath === '') return root;
		return `${root}${selectedContextPath.startsWith('/') ? '' : '/'}${selectedContextPath}`;
	});

	const contextDir = $derived.by(() => {
		if (contextMode === 'remote') {
			return remoteContextSource.trim();
		}
		return workspaceContextDir;
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

	const { inputs, ...form } = createForm<typeof formSchema>(formSchema, {
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
		provider: ($settingsStore?.buildProvider as 'local' | 'depot') ?? 'local',
		push: false,
		load: true
	});

	let isBuilding = $state(false);
	let isDesktop = $state(true);
	let buildStatusText = $state('');
	let buildError = $state('');
	let hasReachedComplete = $state(false);
	let logLines = $state<string[]>([]);
	let autoScroll = $state(true);
	type MainTab = 'build' | 'history';
	const mainUrlTab = useUrlTab<MainTab>({
		validTabs: () => ['build', 'history'],
		defaultTab: () => 'build'
	});
	const mainTab = $derived(mainUrlTab.value);
	let buildTab = $state('workspace');
	let rightPanelTab = $state<'config' | 'output'>('config');
	let showAdvanced = $state(false);

	const selectedEnvId = $derived(environmentStore.selected?.id || '0');
	const queryClient = useQueryClient();
	const resolvedProvider = $derived(depotAvailable ? $inputs.provider.value : 'local');
	const isPushMode = $derived(resolvedProvider === 'depot' ? true : $inputs.push.value);

	const registryRequestOptions = {
		pagination: { page: 1, limit: 100 },
		sort: { column: 'url', direction: 'asc' }
	} satisfies SearchPaginationSortRequest;

	const registriesQuery = createQuery(() => ({
		queryKey: queryKeys.containerRegistries.list(registryRequestOptions),
		enabled: isPushMode,
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

	const selectedRegistry = $derived(registries.find((registry) => registry.id === $inputs.registryId.value));

	const repositoryOptions = $derived<SelectOption[]>(
		(selectedRegistry?.repositoryNames ?? []).map((name) => ({ label: name, value: name }))
	);

	const fullImageReference = $derived(
		isPushMode && selectedRegistry
			? buildImageReference(selectedRegistry.url, $inputs.repositoryName.value, $inputs.pushTag.value)
			: ''
	);

	// Clear the repository name when registry changes to prevent cross-registry mismatches.
	let lastRegistryId = $state($inputs.registryId.value);
	$effect(() => {
		const current = $inputs.registryId.value;
		if (current !== lastRegistryId) {
			lastRegistryId = current;
			$inputs.repositoryName.value = '';
		}
	});

	type ImageBuildRequest = { envId: string; provider: 'local' | 'depot' } & Pick<
		ImageBuildRecord,
		| 'contextDir'
		| 'dockerfile'
		| 'tags'
		| 'target'
		| 'buildArgs'
		| 'labels'
		| 'cacheFrom'
		| 'cacheTo'
		| 'noCache'
		| 'pull'
		| 'network'
		| 'isolation'
		| 'shmSize'
		| 'ulimits'
		| 'entitlements'
		| 'privileged'
		| 'extraHosts'
		| 'platforms'
		| 'push'
		| 'load'
	>;

	function handleBuildLine(line: string): boolean {
		if (line.trim() === '') return false;
		try {
			const event = JSON.parse(line) as Record<string, unknown>;
			const errorMsg = extractErrorMessage(event, m.build_failed());
			if (errorMsg) {
				const cleanErrorMsg = sanitizeLogText(errorMsg);
				buildError = cleanErrorMsg;
				buildStatusText = cleanErrorMsg.toLowerCase().startsWith(m.build_failed().toLowerCase())
					? cleanErrorMsg
					: m.build_failed_with_error({ error: cleanErrorMsg });
				appendLog(m.build_error_log({ error: cleanErrorMsg }));
				return false;
			}

			// The terminal frame marks success; don't wait on the network EOF.
			if (event['done'] === true) {
				return true;
			}

			// Raw docker CLI output arrives framed as {"log":"<line>"}.
			if (typeof event['log'] === 'string') {
				appendLog(event['log']);
				buildStatusText = sanitizeLogText(event['log']);
			}
		} catch {
			appendLog(line);
		}
		return false;
	}

	const buildMutation = createMutation(() => ({
		mutationKey: queryKeys.images.buildRun(selectedEnvId),
		mutationFn: async (request: ImageBuildRequest) => {
			const { envId, ...payload } = request;
			const response = await fetch(`/api/environments/${envId}/images/build`, {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json'
				},
				body: JSON.stringify(payload)
			});

			if (!response.ok || !response.body) {
				const errorData = (await response.json().catch(() => ({
					data: { message: m.build_request_failed() }
				}))) as Record<string, unknown>;
				const errorDataPayload = errorData['data'] as Record<string, unknown> | undefined;
				const errorMessage =
					errorDataPayload?.['message'] ||
					errorData['error'] ||
					errorData['message'] ||
					m.build_request_failed_http({ status: response.status });
				throw new Error(String(errorMessage));
			}

			const reader = response.body.getReader();
			const decoder = new TextDecoder();
			let buffer = '';
			let streamComplete = false;

			while (!streamComplete) {
				const { done, value } = await reader.read();
				if (done) {
					break;
				}

				buffer += decoder.decode(value, { stream: true });
				const lines = buffer.split('\n');
				buffer = lines.pop() || '';

				streamComplete = lines.some(handleBuildLine);
			}
			if (streamComplete) {
				await reader.cancel().catch(() => {});
			}

			if (buildError) {
				throw new Error(buildError);
			}

			hasReachedComplete = true;
			buildStatusText = m.build_completed();
			appendLog(m.build_completed());
		},
		onSettled: async (_data, _error, variables) => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.images.builds(variables.envId) });
		}
	}));

	const aggregateStatus = $derived.by(() => {
		if (hasReachedComplete) return m.build_completed();
		if (isBuilding) return buildStatusText || m.progress_building();
		return buildStatusText;
	});
	const statusLabel = $derived.by(() => {
		if (buildError) return m.common_error();
		if (hasReachedComplete) return m.build_completed();
		if (isBuilding) return m.common_live();
		return m.idle();
	});
	const buildMobileTabItems = $derived([
		{ value: 'workspace', label: m.build_context() },
		{ value: 'configuration', label: m.build_configuration() },
		{ value: 'output', label: m.build_output() }
	]);

	const mainTabItems = $derived([
		{ value: 'build', label: m.build_workspace() },
		{ value: 'history', label: m.builds() }
	]);

	onMount(() => {
		const mq = window.matchMedia('(min-width: 1024px)');
		const update = () => {
			isDesktop = mq.matches;
		};

		update();

		mq.addEventListener('change', update);
		return () => mq.removeEventListener('change', update);
	});

	$effect(() => {
		if (!depotAvailable && $inputs.provider.value === 'depot') {
			$inputs.provider.value = 'local';
		}
	});

	function resetState() {
		isBuilding = false;
		buildStatusText = '';
		buildError = '';
		hasReachedComplete = false;
		logLines = [];
	}

	function appendLog(line: string) {
		// Keep ANSI sequences — the output panel renders them as colors.
		if (sanitizeLogText(line).trim() === '') return;
		logLines = [...logLines, line];
	}

	function parseBuildArgs(raw: string): Record<string, string> {
		const result: Record<string, string> = {};
		for (const line of raw.split('\n')) {
			const trimmed = line.trim();
			if (!trimmed) continue;
			const idx = trimmed.indexOf('=');
			if (idx === -1) continue;
			const key = trimmed.slice(0, idx).trim();
			const value = trimmed.slice(idx + 1).trim();
			if (!key) continue;
			result[key] = value;
		}
		return result;
	}

	function parseOptionalBytes(raw: string): number | undefined {
		const trimmed = raw.trim();
		if (!trimmed) return undefined;
		const parsed = Number.parseInt(trimmed, 10);
		if (!Number.isFinite(parsed) || parsed <= 0) return undefined;
		return parsed;
	}

	// Push builds target a configured registry, so their reference is assembled
	// from the selected registry, repository name and tag rather than the
	// free-form tags field.
	function resolveBuildTags(
		data: z.infer<typeof formSchema>,
		push: boolean
	): { tags: string[]; error?: undefined } | { tags?: undefined; error: string } {
		if (!push) {
			const tags = parseList(data.tags);
			return tags.length > 0 ? { tags } : { error: m.image_tags_required() };
		}

		const registry = registries.find((item) => item.id === data.registryId);
		if (!registry) return { error: m.select_a_registry() };

		const repositoryName = data.repositoryName.trim();
		if (!repositoryName) return { error: m.select_a_repository_name() };
		if (!registry.repositoryNames?.includes(repositoryName)) return { error: m.repository_name_not_configured() };

		const reference = buildImageReference(registry.url, repositoryName, data.pushTag);
		if (!reference) return { error: m.tag_required() };

		return { tags: [reference] };
	}

	function validateProviderCompatibility(
		provider: 'local' | 'depot',
		values: {
			cacheTo: string[];
			entitlements: string[];
			privileged: boolean;
			platforms: string[];
			network?: string;
			isolation?: string;
			shmSize?: number;
			ulimits: Record<string, string>;
			extraHosts: string[];
		}
	): string | null {
		const unsupportedFields =
			provider === 'local'
				? {
						cacheTo: values.cacheTo.length > 0,
						entitlements: values.entitlements.length > 0,
						privileged: values.privileged,
						platforms: values.platforms.length > 1
					}
				: {
						network: Boolean(values.network),
						isolation: Boolean(values.isolation),
						shmSize: Boolean(values.shmSize),
						ulimits: Object.keys(values.ulimits).length > 0,
						extraHosts: values.extraHosts.length > 0
					};
		const unsupported = Object.entries(unsupportedFields)
			.filter(([, present]) => present)
			.map(([field]) => field);
		if (unsupported.length === 0) return null;
		const fields = unsupported.sort().join(', ');
		return provider === 'local' ? m.build_provider_unsupported_local({ fields }) : m.build_provider_unsupported_depot({ fields });
	}

	function applyBuildConfig(build: ImageBuildRecord) {
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

		showAdvanced =
			[build.dockerfile, build.target, build.network, build.isolation, build.privileged, build.noCache, build.pull].some(
				Boolean
			) ||
			[build.platforms, build.cacheFrom, build.cacheTo, build.entitlements, build.extraHosts].some(
				(values) => (values?.length ?? 0) > 0
			) ||
			[build.buildArgs, build.labels, build.ulimits].some((values) => Object.keys(values ?? {}).length > 0) ||
			(build.shmSize ?? 0) > 0;

		if (isGitBuildContextSource(build.contextDir)) {
			contextMode = 'remote';
			remoteContextSource = build.contextDir;
		} else {
			contextMode = 'workspace';
			remoteContextSource = '';
			selectedContextPath = getContextPathFromBuild(build, buildsRoot);
		}
		mainUrlTab.select('build');
		rightPanelTab = 'config';
		buildTab = 'configuration';
	}

	async function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		if (!contextDir || contextDir.trim() === '') {
			toast.error(contextMode === 'remote' ? m.build_remote_context_required() : m.build_context_required());
			return;
		}

		resetState();
		// Prefer showing progress immediately once a build begins.
		rightPanelTab = 'output';
		buildTab = 'output';
		isBuilding = true;
		buildStatusText = m.starting_build();
		appendLog(m.using_context({ context: contextDir }));

		const resolvedProvider = depotAvailable ? data.provider : 'local';
		const push = resolvedProvider === 'depot' ? true : data.push;
		const load = resolvedProvider === 'depot' ? false : data.load;

		const resolvedTags = resolveBuildTags(data, push);
		if (resolvedTags.error) {
			toast.error(resolvedTags.error);
			isBuilding = false;
			return;
		}
		const tags = resolvedTags.tags;

		const parsedCacheTo = parseList(data.cacheTo);
		const parsedEntitlements = parseList(data.entitlements);
		const parsedPlatforms = parseList(data.platforms);
		const parsedExtraHosts = parseList(data.extraHosts);
		const parsedUlimits = parseBuildArgs(data.ulimits);
		const parsedShmSize = parseOptionalBytes(data.shmSize);

		const network = data.network.trim() || undefined;
		const isolation = data.isolation.trim() || undefined;

		const providerValidationError = validateProviderCompatibility(resolvedProvider, {
			cacheTo: parsedCacheTo,
			entitlements: parsedEntitlements,
			privileged: data.privileged,
			platforms: parsedPlatforms,
			network,
			isolation,
			shmSize: parsedShmSize,
			ulimits: parsedUlimits,
			extraHosts: parsedExtraHosts
		});
		if (providerValidationError) {
			toast.error(providerValidationError);
			isBuilding = false;
			return;
		}

		const payload = {
			contextDir: contextDir.trim(),
			dockerfile: data.dockerfile.trim() || undefined,
			tags,
			target: data.target.trim() || undefined,
			buildArgs: parseBuildArgs(data.buildArgs),
			labels: parseBuildArgs(data.labels),
			cacheFrom: parseList(data.cacheFrom),
			cacheTo: parsedCacheTo,
			noCache: data.noCache,
			pull: data.pull,
			network,
			isolation,
			shmSize: parsedShmSize,
			ulimits: parsedUlimits,
			entitlements: parsedEntitlements,
			privileged: data.privileged,
			extraHosts: parsedExtraHosts,
			platforms: parsedPlatforms,
			provider: resolvedProvider,
			push,
			load
		};

		try {
			const resolvedEnvId = await environmentStore.getCurrentEnvironmentId();
			await buildMutation.mutateAsync({ envId: resolvedEnvId, ...payload });
			toast.success(m.build_completed());
		} catch (error) {
			const message = sanitizeLogText(error instanceof Error ? error.message : m.build_failed());
			buildError = message;
			buildStatusText = message.toLowerCase().startsWith(m.build_failed().toLowerCase())
				? message
				: m.build_failed_with_error({ error: message });
			appendLog(m.build_error_log({ error: message }));
			toast.error(message);
		} finally {
			isBuilding = false;
		}
	}

	function onBuildTabChange(value: string) {
		buildTab = value;
	}

	function onMainTabChange(value: string) {
		mainUrlTab.select(value);
	}
</script>

{#snippet workspaceCard()}
	<Card.Root class="flex h-full flex-col overflow-hidden">
		<BuildWorkspacePanel
			rootLabel={buildsRootLabel}
			rootPath={buildsRoot}
			{contextMode}
			{contextDir}
			remoteContext={remoteContextSource}
			onModeChange={(mode) => (contextMode = mode)}
			onRemoteContextChange={(value: string) => (remoteContextSource = value)}
			onSelectContext={(path: string) => (selectedContextPath = path)}
		/>
	</Card.Root>
{/snippet}

{#snippet configPanel()}
	<BuildConfigPanel
		{inputs}
		provider={$inputs.provider.value}
		bind:showAdvanced
		{isPushMode}
		{registryOptions}
		{repositoryOptions}
		{fullImageReference}
		registryLoadFailed={Boolean(registriesQuery.error)}
		onSubmit={handleSubmit}
	/>
{/snippet}

{#snippet historyContent()}
	<ImageBuildHistoryPanel environmentId={selectedEnvId} onApplyBuild={applyBuildConfig} />
{/snippet}
<!-- Right panel with tabs (config + output) -->
{#snippet rightPanel()}
	<div class="flex h-full flex-col">
		<Tabs.Root bind:value={rightPanelTab} class="flex h-full flex-col">
			<!-- Tabs header with refined styling -->
			<div class="flex shrink-0 items-center justify-between border-b border-border/50 bg-muted/40 px-3 py-2">
				<Tabs.List class="flex items-center gap-1 rounded-lg border border-border/60 bg-muted/60 p-1">
					<Tabs.Trigger
						value="config"
						class="rounded-md px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground data-[state=active]:bg-primary/10 data-[state=active]:text-foreground"
					>
						<CodeIcon class="mr-2 size-3.5" />
						{m.build_configuration()}
					</Tabs.Trigger>
					<Tabs.Trigger
						value="output"
						class="rounded-md px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground data-[state=active]:bg-primary/10 data-[state=active]:text-foreground"
					>
						<TerminalIcon class="mr-2 size-3.5" />
						{m.build_output()}
						{#if logLines.length > 0}
							<span
								class="ml-1.5 rounded-full bg-primary/15 px-1.5 py-0.5 text-[10px] font-semibold text-primary ring-1 ring-primary/20"
							>
								{logLines.length}
							</span>
						{/if}
					</Tabs.Trigger>
				</Tabs.List>

				<div class="flex items-center gap-3 pr-2">
					<BuildControls {inputs} {providerOptions} {isBuilding} onBuild={handleSubmit} />
					<div class="hidden h-4 w-px bg-border xl:block"></div>
					<div class="flex items-center gap-2">
						<div class="relative flex items-center">
							<div
								class={`size-2 rounded-full transition-all ${
									buildError
										? 'bg-red-500 shadow-lg shadow-red-500/50'
										: hasReachedComplete
											? 'bg-green-500 shadow-lg shadow-green-500/50'
											: isBuilding
												? 'animate-pulse bg-blue-500 shadow-lg shadow-blue-500/50'
												: 'bg-zinc-600'
								}`}
							></div>
							{#if isBuilding}
								<div class="absolute inset-0 size-2 animate-ping rounded-full bg-blue-500 opacity-75"></div>
							{/if}
						</div>
						<span class="text-xs font-medium text-muted-foreground">{statusLabel}</span>
					</div>
				</div>
			</div>

			<Tabs.Content value="config" class="mt-0 min-h-0 flex-1 overflow-auto">
				{@render configPanel()}
			</Tabs.Content>
			<Tabs.Content value="output" class="mt-0 min-h-0 flex-1 overflow-hidden">
				<BuildOutputPanel
					{logLines}
					{aggregateStatus}
					{hasReachedComplete}
					{buildError}
					{isBuilding}
					bind:autoScroll
					onReset={resetState}
				/>
			</Tabs.Content>
		</Tabs.Root>
	</div>
{/snippet}

{#if isDesktop}
	<ResourceDetailLayout title={m.build_workspace()} subtitle={m.manual_build_workspace_subtitle()}>
		<Tabs.Root value={mainTab} onValueChange={mainUrlTab.select} class="flex h-[calc(100vh-12rem)] flex-col">
			<Tabs.List class="mb-3 flex w-fit gap-2 rounded-lg border border-border/60 bg-muted/60 p-1">
				<Tabs.Trigger
					value="build"
					class="rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground hover:text-foreground data-[state=active]:bg-primary/10 data-[state=active]:text-foreground"
				>
					{m.build_workspace()}
				</Tabs.Trigger>
				<Tabs.Trigger
					value="history"
					class="rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground hover:text-foreground data-[state=active]:bg-primary/10 data-[state=active]:text-foreground"
				>
					{m.build_history()}
				</Tabs.Trigger>
			</Tabs.List>

			<Tabs.Content value="build" class="min-h-0 flex-1">
				<div class="relative flex h-full">
					<ResizableSplit
						class="flex h-full w-full gap-3"
						firstClass="h-full"
						secondClass="h-full"
						minSize={300}
						minSecondSize={520}
						defaultRatio={0.28}
						handleSize={10}
						handleClass="bg-zinc-950/50 rounded-full"
						allowCollapse={true}
						persistKey="arcane.build.workspace.split"
					>
						{#snippet first()}
							{@render workspaceCard()}
						{/snippet}
						{#snippet second()}
							<Card.Root class="flex h-full flex-col overflow-hidden">
								{@render rightPanel()}
							</Card.Root>
						{/snippet}
					</ResizableSplit>
				</div>
			</Tabs.Content>

			<Tabs.Content value="history" class="min-h-0 flex-1">
				<Card.Root class="flex h-full flex-col overflow-hidden">
					{@render historyContent()}
				</Card.Root>
			</Tabs.Content>
		</Tabs.Root>
	</ResourceDetailLayout>
{:else}
	<TabbedPageLayout tabItems={mainTabItems} selectedTab={mainTab} onTabChange={onMainTabChange} class="min-h-[calc(100vh-10rem)]">
		{#snippet headerInfo()}
			<div class="flex flex-col gap-1">
				{#if mainTab === 'history'}
					<h1 class="text-2xl font-semibold tracking-tight">{m.builds()}</h1>
					<p class="text-sm text-muted-foreground">{m.build_output()}</p>
				{:else}
					<h1 class="text-2xl font-semibold tracking-tight">{m.build_workspace()}</h1>
					<p class="text-sm text-muted-foreground">{m.manual_build_workspace_subtitle()}</p>
				{/if}
			</div>
		{/snippet}

		{#snippet headerActions()}
			{#if mainTab === 'build'}
				<BuildControls {inputs} {providerOptions} {isBuilding} onBuild={handleSubmit} />
			{/if}
		{/snippet}

		{#snippet tabContent(tab)}
			<div class="min-h-[60vh]">
				{#if tab === 'build'}
					<TabbedPageLayout
						tabItems={buildMobileTabItems}
						selectedTab={buildTab}
						onTabChange={onBuildTabChange}
						class="min-h-[60vh]"
					>
						{#snippet headerInfo()}
							<div class="flex flex-col gap-1">
								<h2 class="text-lg font-semibold">{m.build_workspace()}</h2>
								<p class="text-xs text-muted-foreground">{m.manual_build_workspace_subtitle()}</p>
							</div>
						{/snippet}
						{#snippet headerActions()}
							<BuildControls {inputs} {providerOptions} {isBuilding} onBuild={handleSubmit} />
						{/snippet}
						{#snippet tabContent(buildTabValue)}
							{#if buildTabValue === 'workspace'}
								{@render workspaceCard()}
							{:else if buildTabValue === 'configuration'}
								<Card.Root class="overflow-hidden">
									{@render configPanel()}
								</Card.Root>
							{:else}
								<Card.Root class="flex h-full min-h-[500px] flex-col overflow-hidden">
									<BuildOutputPanel
										{logLines}
										{aggregateStatus}
										{hasReachedComplete}
										{buildError}
										{isBuilding}
										bind:autoScroll
										onReset={resetState}
									/>
								</Card.Root>
							{/if}
						{/snippet}
					</TabbedPageLayout>
				{:else}
					<Card.Root class="flex h-full min-h-[500px] flex-col overflow-hidden">
						{@render historyContent()}
					</Card.Root>
				{/if}
			</div>
		{/snippet}
	</TabbedPageLayout>
{/if}
