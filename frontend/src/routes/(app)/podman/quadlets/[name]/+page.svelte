<script lang="ts">
	import { goto } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import ActionButtonGroup from '#lib/components/action-button-group/action-button-group.svelte';
	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import EnvironmentCell from '#lib/components/arcane-table/cells/environment-cell.svelte';
	import { DetailMetaStrip, DetailSection, KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import { AlertTriangleIcon, BoxIcon, CodeIcon, ContainersIcon, FileTextIcon, InfoIcon } from '#lib/icons/index.js';
	import TabbedPageLayout from '#lib/layouts/tabbed-page-layout.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService, type PodmanQuadletAction } from '#lib/services/podman-service.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { environmentHref } from '#lib/utils/environments.js';
	import { podmanStateVariant } from '#lib/utils/podman.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import ContainerLogsPanel from '../../../containers/components/container-logs-panel.svelte';
	import QuadletWorkspaceEditor, {
		createQuadletWorkspace,
		quadletWorkspaceFiles
	} from '../components/quadlet-workspace-editor.svelte';
	import { confirmRemoveQuadlet, quadletActionLabels, runQuadletAction } from '../quadlet-actions';

	let { data } = $props();

	const queryClient = useQueryClient();
	const envId = $derived(data.envId);
	const status = $derived(data.podmanStatuses[data.envId]);
	const systemdAvailable = $derived(status?.systemdAvailable === true);

	const quadletQuery = createQuery(() => ({
		queryKey: queryKeys.podman.quadlet(envId, data.quadlet.name),
		queryFn: () => podmanService.getQuadlet(data.quadlet.name, envId),
		initialData: data.quadlet,
		refetchInterval: 10000,
		retry: false
	}));
	const quadlet = $derived(quadletQuery.data ?? data.quadlet);
	const containers = $derived(quadlet.containers ?? []);
	const podNames = $derived([
		...new Set(containers.map((container) => container.podName).filter((name): name is string => !!name))
	]);

	const canEdit = $derived(hasPermission(PERMISSIONS.PODMAN_QUADLETS, envId));
	const canLifecycle = $derived(hasPermission(PERMISSIONS.PODMAN_QUADLETS_LIFECYCLE, envId) && !quadlet.template);
	const canViewLogs = $derived(hasPermission(PERMISSIONS.CONTAINERS_LOGS, envId));

	let content = $derived(data.file.content);
	let workspace = $state.raw(createQuadletWorkspace());
	let saving = $state(false);
	let pendingAction = $state<PodmanQuadletAction | null>(null);
	let selectedContainerId = $state('');
	let autoScrollLogs = $state(true);
	const selectedContainer = $derived(containers.find((container) => container.id === selectedContainerId) ?? containers[0]);

	const urlTab = useUrlTab({
		tabs: () => [
			{ value: 'overview', label: m.common_overview(), icon: InfoIcon },
			{ value: 'files', label: m.podman_quadlet_files(), icon: CodeIcon },
			{ value: 'containers', label: m.containers(), icon: ContainersIcon },
			{ value: 'logs', label: m.common_logs(), icon: FileTextIcon, visible: canViewLogs && containers.length > 0 }
		],
		defaultTab: () => 'overview'
	});

	async function refresh() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.podman.quadlet(envId, quadlet.name) });
	}

	async function handleSave() {
		const extraFiles = quadletWorkspaceFiles(workspace);
		saving = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				podmanService.updateQuadlet(quadlet.name, { content, extraFiles: extraFiles.length > 0 ? extraFiles : undefined }, envId)
			),
			message: m.common_update_failed({ resource: quadlet.name }),
			setLoadingState: (value) => (saving = value),
			onSuccess: async (report) => {
				const errors = Object.entries(report.quadletErrors ?? {});
				if (errors.length > 0) {
					toast.error(m.common_update_failed({ resource: quadlet.name }), {
						description: errors.map(([name, error]) => `${name}: ${error}`).join('\n')
					});
					return;
				}
				toast.success(m.common_update_success({ resource: quadlet.name }));
				workspace = createQuadletWorkspace();
				await refresh();
			}
		});
	}

	function lifecycleButton(action: PodmanQuadletAction, placement: 'primary' | 'secondary'): ActionButton {
		return {
			id: action,
			action,
			group: 'lifecycle',
			placement,
			label: quadletActionLabels[action](),
			loading: pendingAction === action,
			disabled: pendingAction !== null || !systemdAvailable,
			disabledReason: systemdAvailable ? undefined : status?.systemdReason,
			onclick: () =>
				runQuadletAction(quadlet.name, action, envId, refresh, (loading) => (pendingAction = loading ? action : null))
		};
	}

	const isActive = $derived(podmanStateVariant(quadlet.unitStatus?.activeState ?? quadlet.status) === 'green');

	const actions: ActionButton[] = $derived([
		...(canLifecycle
			? [
					isActive ? lifecycleButton('stop', 'primary') : lifecycleButton('start', 'primary'),
					lifecycleButton('restart', 'secondary')
				]
			: []),
		...(canEdit
			? [
					{
						id: 'remove',
						action: 'remove' as const,
						group: 'danger' as const,
						destructive: true,
						placement: 'menu' as const,
						label: m.common_remove(),
						onclick: () => confirmRemoveQuadlet(quadlet.name, envId, () => goto('/podman/quadlets'))
					}
				]
			: [])
	]);

	const metaItems = $derived([
		{ icon: InfoIcon, label: m.podman_unit(), value: quadlet.unitName || '-', mono: true },
		...(quadlet.unitStatus
			? [
					{ label: m.podman_unit_load_state(), value: quadlet.unitStatus.loadState },
					{
						label: m.podman_unit_active_state(),
						value: `${quadlet.unitStatus.activeState} (${quadlet.unitStatus.subState})`
					}
				]
			: []),
		...(quadlet.app ? [{ icon: BoxIcon, label: m.podman_quadlet_app(), value: quadlet.app }] : [])
	]);
</script>

<TabbedPageLayout
	backUrl="/podman/quadlets"
	backLabel={m.common_back()}
	tabItems={urlTab.items}
	selectedTab={urlTab.value}
	onTabChange={urlTab.select}
>
	{#snippet headerInfo()}
		<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
			<h1 class="max-w-40 min-w-0 truncate text-lg font-semibold sm:max-w-56 md:max-w-72 lg:max-w-88" title={quadlet.name}>
				{quadlet.name}
			</h1>
			<Badge variant={podmanStateVariant(quadlet.status)} minWidth="20">{quadlet.status}</Badge>
			<EnvironmentCell value={envId} />
			{#if quadlet.template}
				<Badge variant="gray" size="sm">{m.podman_quadlet_template()}</Badge>
			{/if}
			{#each podNames as podName (podName)}
				<Badge variant="purple" size="sm" href={environmentHref(`/podman/pods/${encodeURIComponent(podName)}`, envId)}
					>{podName}</Badge
				>
			{/each}
		</div>
	{/snippet}

	{#snippet headerActions()}
		{#if actions.length > 0}
			<ActionButtonGroup buttons={actions} />
		{/if}
	{/snippet}

	{#snippet subHeader()}
		{#if quadletQuery.isError}
			<Alert.Root variant="destructive" class="mt-3">
				<AlertTriangleIcon class="size-4" />
				<Alert.Title>{m.podman_updates_paused()}</Alert.Title>
				<Alert.Description>{extractApiErrorMessage(quadletQuery.error)}</Alert.Description>
			</Alert.Root>
		{/if}
		{#if canLifecycle && !systemdAvailable && status?.systemdReason}
			<p class="mt-2 text-sm text-muted-foreground">{status.systemdReason}</p>
		{/if}
	{/snippet}

	{#snippet tabContent(activeTab)}
		<Tabs.Content value="overview" class="h-full">
			<div class="space-y-4">
				<DetailMetaStrip items={metaItems} />
				<DetailSection title={m.podman_unit()} icon={InfoIcon}>
					<KeyValueGrid>
						{#if quadlet.unitStatus?.description}
							<KeyValueCard label={m.common_description()}>{quadlet.unitStatus.description}</KeyValueCard>
						{/if}
						<KeyValueCard label={m.podman_quadlet_path()}>{quadlet.path}</KeyValueCard>
						{#if quadlet.app}
							<KeyValueCard label={m.podman_quadlet_app()}>{quadlet.app}</KeyValueCard>
						{/if}
						<KeyValueCard label={m.common_type()}>.{quadlet.type}</KeyValueCard>
					</KeyValueGrid>
				</DetailSection>
			</div>
		</Tabs.Content>

		<Tabs.Content value="files" class="h-full">
			<div class="flex h-full min-h-128 flex-col gap-3">
				{#if canEdit}
					<div class="flex shrink-0 items-center justify-between gap-3">
						<p class="text-sm text-muted-foreground">{m.podman_quadlet_update_files_description()}</p>
						<ArcaneButton
							action="save"
							customLabel={m.common_save()}
							onclick={handleSave}
							loading={saving}
							disabled={saving || !content.trim()}
						/>
					</div>
				{/if}
				<QuadletWorkspaceEditor
					{workspace}
					mainFileName={quadlet.name}
					bind:content
					fileId={`podman:quadlets:${envId}:${quadlet.name}`}
					environmentId={envId}
					disabled={saving}
					readOnly={!canEdit}
				/>
			</div>
		</Tabs.Content>

		<Tabs.Content value="containers" class="h-full">
			{#if containers.length === 0}
				<EmptyState icon={ContainersIcon} title={m.podman_quadlet_no_containers()} variant="plain" />
			{:else}
				<Card.Root>
					<div class="divide-y">
						{#each containers as container (container.id)}
							<div class="flex items-center justify-between gap-3 px-4 py-3">
								<div class="flex min-w-0 items-center gap-3">
									<ContainersIcon class="size-4 shrink-0 text-muted-foreground" />
									<div class="min-w-0">
										<a
											class="block truncate font-medium hover:underline"
											href={environmentHref(`/containers/${container.id}`, envId)}
										>
											{container.names[0] ?? container.id.slice(0, 12)}
										</a>
										<p class="truncate font-mono text-xs text-muted-foreground">{container.image}</p>
									</div>
								</div>
								<div class="flex shrink-0 items-center gap-2">
									{#if container.podName}
										<Badge
											variant="purple"
											size="sm"
											href={environmentHref(`/podman/pods/${encodeURIComponent(container.podName)}`, envId)}
										>
											<BoxIcon class="size-3" />
											{container.podName}
										</Badge>
									{/if}
									<Badge variant={podmanStateVariant(container.state)} size="sm">{container.state}</Badge>
									{#if canViewLogs}
										<ArcaneButton
											action="base"
											tone="ghost"
											size="sm"
											customLabel={m.common_logs()}
											onclick={() => {
												selectedContainerId = container.id;
												urlTab.select('logs');
											}}
										/>
									{/if}
								</div>
							</div>
						{/each}
					</div>
				</Card.Root>
			{/if}
		</Tabs.Content>

		{#if canViewLogs && selectedContainer}
			<Tabs.Content value="logs" class="h-full">
				<div class="flex h-full flex-col gap-3">
					{#if containers.length > 1}
						<Select.Root type="single" bind:value={() => selectedContainer.id, (id) => (selectedContainerId = id)}>
							<Select.Trigger class="h-8 w-64">
								<span class="truncate">{selectedContainer.names[0] ?? selectedContainer.id.slice(0, 12)}</span>
							</Select.Trigger>
							<Select.Content>
								{#each containers as container (container.id)}
									<Select.Item value={container.id}>{container.names[0] ?? container.id.slice(0, 12)}</Select.Item>
								{/each}
							</Select.Content>
						</Select.Root>
					{/if}
					{#if activeTab === 'logs'}
						{#key `${envId}:${selectedContainer.id}`}
							<ContainerLogsPanel
								containerId={selectedContainer.id}
								environmentId={envId}
								isRunning={selectedContainer.state === 'running'}
								bind:autoScroll={autoScrollLogs}
							/>
						{/key}
					{/if}
				</div>
			</Tabs.Content>
		{/if}
	{/snippet}
</TabbedPageLayout>
