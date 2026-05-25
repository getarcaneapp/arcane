<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import {
		UsersIcon,
		EnvironmentsIcon,
		EllipsisIcon,
		InspectIcon,
		TrashIcon,
		EdgeConnectionIcon,
		AddIcon,
		CloseIcon,
		TagIcon
	} from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import type { SwarmNodeAgentDeployment, SwarmNodeSummary } from '$lib/types/swarm';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { capitalizeFirstLetter } from '$lib/utils/formatting';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/api';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '$lib/utils/api';
	import { goto } from '$app/navigation';
	import { hasPermission } from '$lib/utils/auth';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import SwarmNodeAgentDialog from './swarm-node-agent-dialog.svelte';
	import SwarmNodeLabelDialog from './swarm-node-label-dialog.svelte';
	import { getSwarmNodeAgentActionLabel, getSwarmNodeAgentLabel, getSwarmNodeAgentVariant } from './agent-status';

	let {
		nodes = $bindable(),
		requestOptions = $bindable()
	}: {
		nodes: Paginated<SwarmNodeSummary>;
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	const currentEnvId = $derived(environmentStore.selected?.id);
	const canManageNodes = $derived(hasPermission('swarm:nodes', currentEnvId));
	let isLoading = $state(false);
	let isAgentDialogOpen = $state(false);
	let selectedNode = $state<SwarmNodeSummary | null>(null);
	let agentDeploymentError = $state('');
	let agentDeployment = $state<SwarmNodeAgentDeployment | null>(null);
	let isAgentDeploymentLoading = $state(false);
	let isAddLabelDialogOpen = $state(false);
	let nodeToLabel = $state<SwarmNodeSummary | null>(null);

	function statusVariant(state: string): 'green' | 'red' | 'amber' | 'gray' {
		if (state === 'ready') return 'green';
		if (state === 'down') return 'red';
		if (state === 'unknown') return 'amber';
		return 'gray';
	}

	function availabilityVariant(state: string): 'green' | 'amber' | 'red' | 'gray' {
		if (state === 'active') return 'green';
		if (state === 'pause') return 'amber';
		if (state === 'drain') return 'red';
		return 'gray';
	}

	async function refreshNodes() {
		nodes = await swarmService.getNodes(requestOptions);
		if (selectedNode) {
			selectedNode = nodes.data.find((node) => node.id === selectedNode?.id) ?? selectedNode;
		}
	}

	async function loadAgentDeployment(node: SwarmNodeSummary, rotate = false) {
		if (isAgentDeploymentLoading) return;

		isAgentDeploymentLoading = true;
		agentDeploymentError = '';

		try {
			agentDeployment = await swarmService.getNodeAgentDeployment(node.id, rotate);
			await refreshNodes();
		} catch (error) {
			agentDeployment = null;
			agentDeploymentError = extractApiErrorMessage(error);
			throw error;
		} finally {
			isAgentDeploymentLoading = false;
		}
	}

	function openAgentDialog(node: SwarmNodeSummary) {
		selectedNode = node;
		agentDeployment = null;
		agentDeploymentError = '';
		isAgentDialogOpen = true;
		void loadAgentDeployment(node).catch(() => undefined);
	}

	function refreshAgentDeployment() {
		if (!selectedNode) return;
		void loadAgentDeployment(selectedNode).catch(() => undefined);
	}

	function regenerateAgentDeployment() {
		const node = selectedNode;
		if (!node) return;

		openConfirmDialog({
			title: m.environments_regenerate_dialog_title(),
			message: m.environments_regenerate_dialog_message(),
			confirm: {
				label: m.environments_regenerate_api_key(),
				destructive: true,
				action: async () => {
					try {
						await loadAgentDeployment(node, true);
						toast.success(m.environments_regenerate_key_success());
					} catch {
						toast.error(m.environments_regenerate_key_failed());
					}
				}
			}
		});
	}

	function inspectNodeTasks(node: SwarmNodeSummary) {
		goto(`/swarm/tasks?nodeId=${encodeURIComponent(node.id)}&search=${encodeURIComponent(node.hostname)}`);
	}

	async function mutateNode(action: () => Promise<void>, successMessage: string, failureMessage: string) {
		await handleApiResultWithCallbacks({
			result: await tryCatch(action()),
			message: failureMessage,
			setLoadingState: (v) => (isLoading = v),
			onSuccess: async () => {
				toast.success(successMessage);
				await refreshNodes();
			}
		});
	}

	async function setAvailability(node: SwarmNodeSummary, availability: 'active' | 'pause' | 'drain') {
		await mutateNode(
			() => swarmService.updateNode(node.id, { availability }),
			m.common_update_success({ resource: m.swarm_node() }),
			m.swarm_node_update_failed({ name: node.hostname })
		);
	}

	async function promoteNode(node: SwarmNodeSummary) {
		await mutateNode(
			() => swarmService.promoteNode(node.id),
			m.swarm_node_promote_success({ name: node.hostname }),
			m.swarm_node_promote_failed({ name: node.hostname })
		);
	}

	async function demoteNode(node: SwarmNodeSummary) {
		await mutateNode(
			() => swarmService.demoteNode(node.id),
			m.swarm_node_demote_success({ name: node.hostname }),
			m.swarm_node_demote_failed({ name: node.hostname })
		);
	}

	function removeNode(node: SwarmNodeSummary) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.swarm_node() }),
			message: m.common_delete_confirm({ resource: m.swarm_node() }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					mutateNode(
						() => swarmService.removeNode(node.id, true),
						m.swarm_node_remove_success({ name: node.hostname }),
						m.swarm_node_remove_failed({ name: node.hostname })
					);
				}
			}
		});
	}

	function openAddLabelDialog(node: SwarmNodeSummary) {
		nodeToLabel = node;
		isAddLabelDialogOpen = true;
	}

	async function addLabel(key: string, value: string) {
		if (!nodeToLabel) return;
		const labels = { ...(nodeToLabel.labels ?? {}), [key]: value };
		await mutateNode(
			() => swarmService.updateNode(nodeToLabel!.id, { labels }),
			m.common_update_success({ resource: m.swarm_node() }),
			m.swarm_node_update_failed({ name: nodeToLabel.hostname })
		);
	}

	function removeLabel(node: SwarmNodeSummary, key: string) {
		openConfirmDialog({
			title: m.common_remove_title({ resource: `label "${key}"` }),
			message: m.common_remove_confirm({ resource: `label "${key}"` }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					const labels = { ...(node.labels ?? {}) };
					delete labels[key];
					await mutateNode(
						() => swarmService.updateNode(node.id, { labels }),
						m.common_update_success({ resource: m.swarm_node() }),
						m.swarm_node_update_failed({ name: node.hostname })
					);
				}
			}
		});
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'hostname', title: m.swarm_hostname(), sortable: true },
		{ accessorKey: 'role', title: m.common_role(), sortable: true, cell: RoleCell },
		{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'availability', title: m.swarm_availability(), sortable: true, cell: AvailabilityCell },
		{ accessorKey: 'labels', title: m.common_labels(), cell: LabelsCell },
		{
			id: 'agent',
			title: m.swarm_node_agent_column(),
			accessorFn: (node) => node.agent?.state ?? 'none',
			cell: AgentCell
		},
		{ accessorKey: 'engineVersion', title: m.swarm_engine_version(), sortable: true }
	] satisfies ColumnSpec<SwarmNodeSummary>[];

	const mobileFields = [
		{ id: 'role', label: m.common_role(), defaultVisible: true },
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'availability', label: m.swarm_availability(), defaultVisible: true },
		{ id: 'labels', label: m.common_labels(), defaultVisible: true },
		{ id: 'agent', label: m.swarm_node_agent_column(), defaultVisible: true },
		{ id: 'engineVersion', label: m.swarm_engine_version(), defaultVisible: false }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet RoleCell({ value }: { value: unknown })}
	<span class="text-sm">{capitalizeFirstLetter(String(value ?? ''))}</span>
{/snippet}

{#snippet StatusCell({ value }: { value: unknown })}
	<StatusBadge text={String(value ?? m.common_unknown())} variant={statusVariant(String(value ?? ''))} />
{/snippet}

{#snippet AvailabilityCell({ value }: { value: unknown })}
	<StatusBadge text={String(value ?? m.common_unknown())} variant={availabilityVariant(String(value ?? ''))} />
{/snippet}

{#snippet AgentCell({ value }: { value: unknown })}
	<StatusBadge
		text={getSwarmNodeAgentLabel(String(value ?? 'none') as SwarmNodeSummary['agent']['state'])}
		variant={getSwarmNodeAgentVariant(String(value ?? 'none') as SwarmNodeSummary['agent']['state'])}
	/>
{/snippet}

{#snippet LabelsCell({ item }: { item: SwarmNodeSummary })}
	<div class="flex flex-wrap items-center gap-1.5">
		{#each Object.entries(item.systemLabels ?? {}) as [key, value] (key)}
			<div class="group relative overflow-hidden rounded-[var(--radius)]">
				<StatusBadge text={`${key}${value ? `=${value}` : ''}`} variant="gray" minWidth="none" class="max-w-[200px] truncate" />
			</div>
		{/each}
		{#each Object.entries(item.labels ?? {}) as [key, value] (key)}
			<div class="group relative overflow-hidden rounded-[var(--radius)]">
				<StatusBadge text={`${key}${value ? `=${value}` : ''}`} variant="blue" minWidth="none" class="max-w-[200px] truncate" />
				{#if canManageNodes}
					<button
						class="absolute inset-0 flex cursor-pointer items-center justify-end rounded-[var(--radius)] bg-blue-500/10 pr-1 opacity-0 backdrop-blur-[1px] transition-opacity group-hover:opacity-100 dark:bg-blue-400/20"
						onclick={() => removeLabel(item, key)}
						title={m.common_remove()}
					>
						<div class="scale-90 rounded-full bg-red-500 p-0.5 shadow-lg transition-transform group-hover:scale-100">
							<CloseIcon class="size-3 text-white" />
						</div>
					</button>
				{/if}
			</div>
		{/each}
		{#if canManageNodes}
			<button
				class="border-border hover:border-primary hover:text-primary inline-flex items-center gap-1 rounded border border-dashed px-2 py-0.5 text-[11px] font-medium transition-colors"
				onclick={() => openAddLabelDialog(item)}
			>
				<AddIcon class="size-3" />
				{m.common_add_button({ resource: 'Label' })}
			</button>
		{/if}
	</div>
{/snippet}

{#snippet NodeMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: SwarmNodeSummary;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: UsersIcon,
			variant: item.role === 'manager' ? 'purple' : 'blue'
		})}
		title={(item: SwarmNodeSummary) => item.hostname}
		subtitle={(item: SwarmNodeSummary) => ((mobileFieldVisibility['engineVersion'] ?? false) ? (item.engineVersion ?? '') : null)}
		badges={[
			(item: SwarmNodeSummary) =>
				(mobileFieldVisibility['status'] ?? true) ? { variant: statusVariant(item.status), text: item.status } : null,
			(item: SwarmNodeSummary) =>
				(mobileFieldVisibility['agent'] ?? true)
					? {
							variant: getSwarmNodeAgentVariant(item.agent?.state),
							text: getSwarmNodeAgentLabel(item.agent?.state)
						}
					: null
		]}
		fields={[
			{
				label: m.common_role(),
				getValue: (item: SwarmNodeSummary) => capitalizeFirstLetter(item.role),
				icon: EnvironmentsIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['role'] ?? true
			},
			{
				label: m.swarm_availability(),
				getValue: (item: SwarmNodeSummary) => capitalizeFirstLetter(item.availability),
				icon: EnvironmentsIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['availability'] ?? true
			},
			{
				label: m.common_labels(),
				getValue: (item: SwarmNodeSummary) =>
					`${Object.keys(item.labels ?? {}).length + Object.keys(item.systemLabels ?? {}).length} labels`,
				icon: TagIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['labels'] ?? true
			},
			{
				label: m.swarm_node_agent_column(),
				getValue: (item: SwarmNodeSummary) => getSwarmNodeAgentLabel(item.agent?.state),
				icon: EdgeConnectionIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['agent'] ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: SwarmNodeSummary })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="relative size-8 p-0">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon />
				</ArcaneButton>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => inspectNodeTasks(item)}>
					<InspectIcon class="size-4" />
					{m.common_inspect()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => openAgentDialog(item)} disabled={!canManageNodes}>
					<EdgeConnectionIcon class="size-4" />
					{getSwarmNodeAgentActionLabel(item.agent?.state)}
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item onclick={() => promoteNode(item)} disabled={!canManageNodes || isLoading || item.role === 'manager'}>
					{m.swarm_node_promote()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => demoteNode(item)} disabled={!canManageNodes || isLoading || item.role !== 'manager'}>
					{m.swarm_node_demote()}
				</DropdownMenu.Item>
				<DropdownMenu.Item
					onclick={() => setAvailability(item, 'drain')}
					disabled={!canManageNodes || isLoading || item.availability === 'drain'}
				>
					{m.swarm_node_drain()}
				</DropdownMenu.Item>
				<DropdownMenu.Item
					onclick={() => setAvailability(item, 'active')}
					disabled={!canManageNodes || isLoading || item.availability === 'active'}
				>
					{m.swarm_node_activate()}
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item variant="destructive" onclick={() => removeNode(item)} disabled={!canManageNodes || isLoading}>
					<TrashIcon class="size-4" />
					{m.common_delete()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-swarm-nodes-table"
	items={nodes}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	onRefresh={async (options) => (nodes = await swarmService.getNodes(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={NodeMobileCardSnippet}
/>

<SwarmNodeAgentDialog
	bind:open={isAgentDialogOpen}
	node={selectedNode}
	deployment={agentDeployment}
	errorMessage={agentDeploymentError}
	isLoading={isAgentDeploymentLoading}
	onRefresh={refreshAgentDeployment}
	onRegenerate={regenerateAgentDeployment}
/>

<SwarmNodeLabelDialog bind:open={isAddLabelDialogOpen} onAdd={addLabel} />
