<script lang="ts">
	import { goto } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import CodePanel from '#lib/components/code-panel.svelte';
	import { DetailSection, KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { BoxIcon, ContainersIcon, InfoIcon } from '#lib/icons/index.js';
	import { ResourceDetailLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService, type PodmanQuadletAction } from '#lib/services/podman-service.js';
	import type { PodmanQuadletFile } from '#lib/types/podman.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { environmentHref } from '#lib/utils/environments.js';
	import { podmanStateVariant } from '#lib/utils/podman.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import ContainerLogsPanel from '../../../containers/components/container-logs-panel.svelte';
	import QuadletExtraFiles from '../components/quadlet-extra-files.svelte';
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

	const canEdit = $derived(hasPermission('podman:quadlets', envId));
	const canLifecycle = $derived(hasPermission('podman:quadlets:lifecycle', envId) && !quadlet.template);

	let content = $derived(data.file.content);
	let extraFiles = $state<PodmanQuadletFile[]>([]);
	let saving = $state(false);
	let pendingAction = $state<PodmanQuadletAction | null>(null);
	let selectedContainerId = $state('');
	let autoScrollLogs = $state(true);
	const selectedContainer = $derived(containers.find((container) => container.id === selectedContainerId) ?? containers[0]);

	async function refresh() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.podman.quadlet(envId, quadlet.name) });
	}

	async function handleSave() {
		const files = extraFiles.filter((file) => file.name.trim());
		saving = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				podmanService.updateQuadlet(quadlet.name, { content, extraFiles: files.length > 0 ? files : undefined }, envId)
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
				extraFiles = [];
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
						id: 'save',
						action: 'save' as const,
						placement: 'secondary' as const,
						label: m.common_save(),
						loading: saving,
						disabled: saving,
						onclick: handleSave
					},
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
</script>

<ResourceDetailLayout
	backUrl="/podman/quadlets"
	backLabel={m.podman_quadlets_title()}
	title={quadlet.name}
	subtitle={quadlet.path}
	{actions}
>
	{#snippet badges()}
		<Badge variant={podmanStateVariant(quadlet.status)} minWidth="20">{quadlet.status}</Badge>
		<Badge variant="gray" size="sm">.{quadlet.type}</Badge>
		{#if quadlet.template}
			<Badge variant="gray" size="sm">{m.podman_quadlet_template()}</Badge>
		{/if}
		{#if quadlet.app}
			<Badge variant="blue" size="sm">{quadlet.app}</Badge>
		{/if}
		{#each podNames as podName (podName)}
			<Badge variant="purple" size="sm" href={environmentHref(`/podman/pods/${encodeURIComponent(podName)}`, envId)}
				>{podName}</Badge
			>
		{/each}
	{/snippet}

	{#if canLifecycle && !systemdAvailable && status?.systemdReason}
		<p class="text-sm text-muted-foreground">{status.systemdReason}</p>
	{/if}

	<DetailSection title={m.podman_unit()} icon={InfoIcon}>
		<KeyValueGrid>
			<KeyValueCard label={m.podman_unit()}>{quadlet.unitName || '-'}</KeyValueCard>
			{#if quadlet.unitStatus}
				<KeyValueCard label={m.podman_unit_load_state()}>{quadlet.unitStatus.loadState}</KeyValueCard>
				<KeyValueCard label={m.podman_unit_active_state()}>
					{quadlet.unitStatus.activeState} ({quadlet.unitStatus.subState})
				</KeyValueCard>
				{#if quadlet.unitStatus.description}
					<KeyValueCard label={m.common_description()}>{quadlet.unitStatus.description}</KeyValueCard>
				{/if}
			{/if}
		</KeyValueGrid>
	</DetailSection>

	<CodePanel title={quadlet.name} language="systemd" validationMode="none" bind:value={content} readOnly={!canEdit} autoHeight />

	{#if canEdit}
		<Card.Root>
			<Card.Header>
				<Card.Title>{m.podman_quadlet_extra_files()}</Card.Title>
				<Card.Description>{m.podman_quadlet_update_files_description()}</Card.Description>
			</Card.Header>
			<Card.Content>
				<QuadletExtraFiles bind:files={extraFiles} disabled={saving} />
			</Card.Content>
		</Card.Root>
	{/if}

	<DetailSection title={m.containers()} icon={ContainersIcon}>
		{#if containers.length === 0}
			<EmptyState icon={ContainersIcon} title={m.podman_quadlet_no_containers()} variant="plain" />
		{:else}
			<div class="divide-y rounded-lg border">
				{#each containers as container (container.id)}
					<div class="flex items-center justify-between gap-3 p-3">
						<button
							type="button"
							class="min-w-0 truncate text-left font-medium hover:underline"
							onclick={() => (selectedContainerId = container.id)}
						>
							{container.names[0] ?? container.id.slice(0, 12)}
						</button>
						<div class="flex items-center gap-2">
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
							<a
								class="text-xs text-muted-foreground hover:underline"
								href={environmentHref(`/containers/${container.id}`, envId)}>{m.common_inspect()}</a
							>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</DetailSection>

	{#if selectedContainer && hasPermission('containers:logs', envId)}
		{#key selectedContainer.id}
			<ContainerLogsPanel
				containerId={selectedContainer.id}
				environmentId={envId}
				isRunning={selectedContainer.state === 'running'}
				bind:autoScroll={autoScrollLogs}
			/>
		{/key}
	{/if}
</ResourceDetailLayout>
