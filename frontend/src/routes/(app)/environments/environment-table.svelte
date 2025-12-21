<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { goto } from '$app/navigation';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { toast } from 'svelte-sonner';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import type { Environment } from '$lib/types/environment.type';
	import { m } from '$lib/paraglide/messages';
	import { environmentManagementService } from '$lib/services/env-mgmt-service';
	import environmentUpgradeService from '$lib/services/api/environment-upgrade-service';
	import UpgradeConfirmationDialog from '$lib/components/dialogs/upgrade-confirmation-dialog.svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';
	import {
		EllipsisIcon,
		EyeOnIcon,
		TrashIcon,
		EnvironmentsIcon,
		InspectIcon,
		UpdateIcon,
		StatsIcon,
		EyeOffIcon,
		TestIcon
	} from '$lib/icons';

	let {
		environments = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable()
	}: {
		environments: Paginated<Environment>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	let isLoading = $state({ removing: false, testing: false, upgrading: false, toggling: false });
	let upgradingEnvironmentId = $state<string | null>(null);
	let showUpgradeDialog = $state(false);
	let selectedEnvironmentForUpgrade = $state<Environment | null>(null);

	async function handleDeleteSelected(ids: string[]) {
		if (!ids?.length) return;

		openConfirmDialog({
			title: m.environments_remove_selected_title({ count: ids.length }),
			message: m.environments_remove_selected_message({ count: ids.length }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					let successCount = 0;
					let failureCount = 0;

					for (const id of ids) {
						const result = await tryCatch(environmentManagementService.delete(id));
						handleApiResultWithCallbacks({
							result,
							message: m.common_bulk_remove_failed({ count: ids.length, resource: m.environments_title() }),
							setLoadingState: () => {},
							onSuccess: () => {
								successCount++;
							}
						});
						if (result.error) failureCount++;
					}

					isLoading.removing = false;

					if (successCount > 0) {
						const msg = m.common_bulk_remove_success({ count: successCount, resource: m.environments_title() });
						toast.success(msg);
						environments = await environmentManagementService.getEnvironments(requestOptions);
						await environmentStore.initialize(environments.data);
					}
					if (failureCount > 0) {
						const msg = m.common_bulk_remove_failed({ count: failureCount, resource: m.environments_title() });
						toast.error(msg);
					}

					selectedIds = [];
				}
			}
		});
	}

	async function handleDeleteOne(id: string, hostname: string) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.resource_environment() }),
			message: m.environments_delete_message({ name: hostname }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					const result = await tryCatch(environmentManagementService.delete(id));
					handleApiResultWithCallbacks({
						result,
						message: m.environments_delete_failed({ name: hostname }),
						setLoadingState: () => {},
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.resource_environment()} "${hostname}"` }));
							environments = await environmentManagementService.getEnvironments(requestOptions);
							await environmentStore.initialize(environments.data);
						}
					});
					isLoading.removing = false;
				}
			}
		});
	}

	async function handleTest(id: string) {
		isLoading.testing = true;
		const result = await tryCatch(environmentManagementService.testConnection(id));
		handleApiResultWithCallbacks({
			result,
			message: m.environments_test_connection_failed(),
			setLoadingState: () => {},
			onSuccess: async (resp) => {
				const status = (resp as { status: string; message?: string }).status;
				if (status === 'online') toast.success(m.environments_test_connection_success());
				else toast.error(m.environments_test_connection_error());
				// Refresh to get updated status from backend
				environments = await environmentManagementService.getEnvironments(requestOptions);
			}
		});
		isLoading.testing = false;
	}

	async function handleUpgradeClick(environment: Environment) {
		selectedEnvironmentForUpgrade = environment;
		showUpgradeDialog = true;
	}

	async function handleConfirmUpgrade() {
		if (!selectedEnvironmentForUpgrade) return;

		const envId = selectedEnvironmentForUpgrade.id;
		const envName = selectedEnvironmentForUpgrade.name;

		isLoading.upgrading = true;
		upgradingEnvironmentId = envId;
		showUpgradeDialog = false;

		try {
			const result = await environmentUpgradeService.triggerEnvironmentUpgrade(envId);

			if (result.success) {
				toast.success(m.upgrade_success());
				toast.info(`Environment "${envName}" is upgrading and will restart shortly.`);
			} else {
				toast.error(result.error || m.upgrade_failed({ error: 'Unknown error' }));
			}
		} catch (error: any) {
			const errorMessage = error?.response?.data?.error || error?.message || 'Unknown error';
			toast.error(m.upgrade_failed({ error: errorMessage }));
		} finally {
			isLoading.upgrading = false;
			upgradingEnvironmentId = null;
			selectedEnvironmentForUpgrade = null;
		}
	}

	async function handleToggleEnabled(environment: Environment) {
		const newEnabled = !environment.enabled;
		isLoading.toggling = true;

		const result = await tryCatch(environmentManagementService.update(environment.id, { enabled: newEnabled }));

		handleApiResultWithCallbacks({
			result,
			message: m.common_update_failed({ resource: m.resource_environment() }),
			setLoadingState: () => {},
			onSuccess: async () => {
				toast.success(
					m.common_update_success({
						resource: `${m.resource_environment()} "${environment.name}"`
					})
				);
				environments = await environmentManagementService.getEnvironments(requestOptions);
				await environmentStore.initialize(environments.data);
			}
		});

		isLoading.toggling = false;
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{
			id: 'name',
			title: m.common_name(),
			sortable: true,
			accessorFn: (row) => row.name,
			cell: EnvironmentCell
		},
		{
			accessorKey: 'status',
			title: m.common_status(),
			sortable: true,
			cell: StatusCell
		},
		{
			accessorKey: 'enabled',
			title: m.common_enabled(),
			sortable: true,
			cell: EnabledCell
		},
		{
			accessorKey: 'apiUrl',
			title: m.environments_api_url(),
			cell: ApiCell
		}
	] satisfies ColumnSpec<Environment>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'enabled', label: m.common_enabled(), defaultVisible: true },
		{ id: 'apiUrl', label: m.environments_api_url(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet EnvironmentCell({ item }: { item: Environment })}
	<div class="flex items-center gap-3">
		<div class="relative">
			<div class="bg-muted flex size-8 items-center justify-center rounded-lg">
				<EnvironmentsIcon class="text-muted-foreground size-4" />
			</div>
			<div
				class="border-background absolute -top-1 -right-1 size-3 rounded-full border-2 {item.status === 'online'
					? 'bg-green-500'
					: 'bg-red-500'}"
			></div>
		</div>
		{#if environmentStore.selected?.id === item.id}
			<Badge variant="default" class="border-blue-500/20 bg-blue-500/10 text-blue-600">
				{m.common_current()}
			</Badge>
		{/if}
		<div class="flex flex-col gap-0.5 leading-tight">
			<button
				class="text-foreground-primary h-auto min-h-0 cursor-pointer p-0 text-left text-sm leading-tight font-medium hover:underline"
				onclick={() => goto(`/environments/${item.id}`)}
			>
				{item.name}
			</button>
			<span class="text-muted-foreground font-mono text-xs leading-tight">{item.apiUrl}</span>
		</div>
	</div>
{/snippet}

{#snippet StatusCell({ value }: { value: unknown })}
	{@const statusValue = String(value)}
	{@const variant = statusValue === 'online' ? 'green' : statusValue === 'pending' ? 'amber' : 'red'}
	<StatusBadge text={capitalizeFirstLetter(statusValue) || m.common_unknown()} {variant} />
{/snippet}

{#snippet ApiCell({ value }: { value: unknown })}
	<span class="text-muted-foreground font-mono text-sm">{String(value)}</span>
{/snippet}

{#snippet EnabledCell({ value }: { value: unknown })}
	<StatusBadge text={value ? m.common_enabled() : m.common_disabled()} variant={value ? 'green' : 'red'} />
{/snippet}

{#snippet EnvironmentMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: Environment;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: StatsIcon, variant: 'emerald' }}
		title={(item: Environment) => item.name || item.id}
		subtitle={(item: Environment) => ((mobileFieldVisibility.id ?? true) ? item.id : null)}
		badges={[
			{ variant: 'green', text: m.sidebar_environment_label() },
			...(environmentStore.selected?.id === item.id ? [{ variant: 'blue' as const, text: m.common_current() }] : [])
		]}
		fields={[
			{
				label: m.environments_api_url(),
				getValue: (item: Environment) => item.apiUrl,
				icon: StatsIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility.apiUrl ?? true) && !!item.apiUrl
			}
		]}
		rowActions={RowActions}
		onclick={(item: Environment) => goto(`/environments/${item.id}`)}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: Environment })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<ArcaneButton
					{...props}
					action="base"
					tone="ghost"
					size="icon"
					class="relative size-8 p-0"
					icon={EllipsisIcon}
					showLabel={false}
					customLabel={m.common_open_menu()}
				/>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item
					onclick={async () => {
						if (!item.enabled) {
							toast.error(m.environments_cannot_switch_disabled());
							return;
						}
						try {
							await environmentStore.setEnvironment(item);
							toast.success(m.environments_switched_to({ name: item.name }));
						} catch (error) {
							console.error('Failed to set environment:', error);
						}
					}}
					disabled={!item.enabled || environmentStore.selected?.id === item.id}
				>
					<EnvironmentsIcon class="size-4" />
					{environmentStore.selected?.id === item.id ? m.environments_current_environment() : m.environments_use_environment()}
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item onclick={() => goto(`/environments/${item.id}`)}>
					<InspectIcon class="size-4" />
					{m.common_view_details()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => handleTest(item.id)} disabled={isLoading.testing}>
					<TestIcon class="size-4" />
					{m.environments_test_connection()}
				</DropdownMenu.Item>
				{#if item.id !== '0'}
					{#if item.status === 'online'}
						<DropdownMenu.Separator />
						<DropdownMenu.Item
							onclick={() => handleUpgradeClick(item)}
							disabled={isLoading.upgrading || upgradingEnvironmentId === item.id}
						>
							<UpdateIcon class="size-4" />
							{upgradingEnvironmentId === item.id ? m.upgrade_in_progress() : m.upgrade_to_version({ version: 'Latest' })}
						</DropdownMenu.Item>
					{/if}
					<DropdownMenu.Item onclick={() => handleToggleEnabled(item)} disabled={isLoading.toggling}>
						{#if item.enabled}
							<EyeOffIcon class="size-4" />
							{m.common_disable()}
						{:else}
							<EyeOnIcon class="size-4" />
							{m.common_disable()}
						{/if}
					</DropdownMenu.Item>
					<DropdownMenu.Separator />
					<DropdownMenu.Item
						variant="destructive"
						onclick={() => handleDeleteOne(item.id, item.name)}
						disabled={isLoading.removing}
					>
						<TrashIcon class="size-4" />
						{m.common_delete()}
					</DropdownMenu.Item>
				{/if}
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-environments-table"
	items={environments}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRemoveSelected={(ids) => handleDeleteSelected(ids)}
	onRefresh={async (options) => (environments = await environmentManagementService.getEnvironments(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={EnvironmentMobileCardSnippet}
/>

<UpgradeConfirmationDialog
	bind:open={showUpgradeDialog}
	version="Latest"
	onConfirm={handleConfirmUpgrade}
	environmentName={selectedEnvironmentForUpgrade?.name}
	environmentId={selectedEnvironmentForUpgrade?.id}
	bind:upgrading={isLoading.upgrading}
/>
