<script lang="ts">
	import { useQueryClient } from '@tanstack/svelte-query';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import CheckedAtCell from '#lib/components/arcane-table/cells/checked-at-cell.svelte';
	import DigestCell from '#lib/components/arcane-table/cells/digest-cell.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import {
		UniversalMobileCard,
		type BulkAction,
		type ColumnSpec,
		type MobileFieldVisibility
	} from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { ContainersIcon, UpdateIcon, EyeOffIcon, EyeOnIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerService } from '#lib/services/container-service.js';
	import type { ContainersPaginatedResponse, ContainerListRequestOptions } from '#lib/services/container-service.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { ContainerSummaryDto } from '#lib/types/docker.js';
	import type { ImageUpdateInfoDto } from '#lib/types/docker.js';
	import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { bulkConfirmAndRun } from '#lib/utils/bulk-actions.js';
	import { confirmAndUpdateContainer } from '#lib/utils/container-actions.js';
	import { isAutoUpdateLabelDisabled, UPDATE_CHECK_LABEL } from '#lib/utils/container-auto-update.js';
	import { createContainerUpdateActivityTracker } from '#lib/utils/container-update-activities.js';
	import {
		environmentHref,
		hasShownPermission,
		listScopes,
		rowKey,
		type EnvironmentPage,
		type EnvironmentRow
	} from '#lib/utils/environments.js';
	import { formatImageUpdateCheckedAt, formatImageUpdateValue } from '#lib/utils/image-updates.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import { getContainerDisplayName } from '../../containers/container-table.helpers';

	type ContainerRow = EnvironmentRow<ContainerSummaryDto>;
	type ContainerUpdateRow = {
		id: string;
		environmentId: string;
		environmentName: string;
		environmentTagged: true;
		containerId: string;
		name: string;
		imageRef: string;
		currentValue: string;
		latestValue: string;
		checkedAt: string;
		/** Automatic installation is off; checks and notifications still run. */
		autoUpdateDisabled: boolean;
		labelControlled: boolean;
		/** False when the agent omitted `autoUpdateEnabled`; toggling is then unavailable. */
		statusAvailable: boolean;
		autoUpdateTitle: string;
		updateInfo?: ImageUpdateInfoDto;
		container: ContainerRow;
	};

	interface Props {
		containers: EnvironmentPage<ContainersPaginatedResponse>;
		requestOptions: SearchPaginationSortRequest;
		// Refreshed rows arrive through the `containers` prop once the parent's query updates.
		onRefreshData: (options: ContainerListRequestOptions) => Promise<void>;
		onAutoUpdateChanged?: () => Promise<unknown> | unknown;
	}

	let { containers, requestOptions = $bindable(), onRefreshData, onAutoUpdateChanged }: Props = $props();

	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});
	let updatingContainerIds = $state<Record<string, boolean>>({});
	let togglingAutoUpdateIds = $state<Record<string, boolean>>({});
	let bulkUpdating = $state(false);
	let updateTableMounted = false;
	const queryClient = useQueryClient();
	const scope = $derived(environmentStore.shownKey);
	const updateActivities = createContainerUpdateActivityTracker(
		() => environmentStore.shown.map((env) => env.id),
		() => {
			if (!updateTableMounted) return;
			void refreshRows();
			// Compose containers also drive the project update badges.
			for (const listScope of listScopes(environmentStore.shown.map((env) => env.id))) {
				void queryClient.invalidateQueries({ queryKey: ['projects', listScope] });
			}
			for (const env of environmentStore.shown) {
				void queryClient.invalidateQueries({ queryKey: ['project', env.id] });
			}
		}
	);
	onMount(() => {
		updateTableMounted = true;
		const unsubscribe = activityStore.subscribeActivities(updateActivities.observe);
		return () => {
			updateTableMounted = false;
			unsubscribe();
		};
	});

	function mapContainerRow(container: ContainerRow): ContainerUpdateRow {
		const name = getContainerDisplayName(container);
		const labelControlled = isAutoUpdateLabelDisabled(container.labels);
		const statusAvailable = typeof container.autoUpdateEnabled === 'boolean';
		const autoUpdateDisabled = container.autoUpdateEnabled === false;
		let autoUpdateTitle = m.updates_disable_auto_update_description();
		if (!statusAvailable) {
			autoUpdateTitle = m.auto_update_status_unavailable();
		} else if (labelControlled) {
			autoUpdateTitle = m.auto_update_controlled_by_label_checks_continue({ label: `${UPDATE_CHECK_LABEL}=false` });
		} else if (autoUpdateDisabled) {
			autoUpdateTitle = m.auto_update_disabled_checks_continue();
		}
		return {
			id: container.id,
			environmentId: container.environmentId,
			environmentName: container.environmentName,
			environmentTagged: true,
			containerId: container.id,
			name,
			imageRef: container.image,
			currentValue: formatImageUpdateValue(container.updateInfo, 'current'),
			latestValue: formatImageUpdateValue(container.updateInfo, 'latest'),
			checkedAt: container.updateInfo?.checkTime ?? '',
			autoUpdateDisabled,
			labelControlled,
			statusAvailable,
			autoUpdateTitle,
			updateInfo: container.updateInfo,
			container
		};
	}

	const tableItems = $derived<Paginated<ContainerUpdateRow, ContainersPaginatedResponse['counts']>>({
		...containers,
		data: (containers.data ?? []).map(mapContainerRow)
	});

	const columns = $derived([
		...environmentColumns<ContainerUpdateRow>(),
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'imageRef', title: m.common_image(), sortable: true, cell: ImageCell },
		{ accessorKey: 'currentValue', title: m.common_current(), sortable: false, cellComponent: DigestCell },
		{ accessorKey: 'latestValue', title: m.image_update_latest_label(), sortable: false, cellComponent: DigestCell },
		{ accessorKey: 'checkedAt', title: m.common_updated(), sortable: false, cellComponent: CheckedAtCell }
	] satisfies ColumnSpec<ContainerUpdateRow>[]);

	const mobileFields = [
		{ id: 'imageRef', label: m.common_image(), defaultVisible: true },
		{ id: 'currentValue', label: m.common_current(), defaultVisible: true },
		{ id: 'latestValue', label: m.image_update_latest_label(), defaultVisible: true },
		{ id: 'checkedAt', label: m.common_updated(), defaultVisible: true }
	];

	async function refreshRows() {
		await onRefreshData(requestOptions as ContainerListRequestOptions);
	}

	async function handleUpdateContainer(container: ContainerRow) {
		const containerName = getContainerDisplayName(container);

		confirmAndUpdateContainer({
			containerId: container.id,
			containerName,
			environmentId: container.environmentId,
			setLoading: (loading) => {
				updatingContainerIds = { ...updatingContainerIds, [rowKey(container)]: loading };
			},
			onRefresh: () => (updateTableMounted ? refreshRows() : undefined),
			onAccepted: (activity) => {
				if (updateTableMounted) updateActivities.accept(activity);
			}
		});
	}

	// Disabling writes to the shared `autoUpdateExcludedContainers` setting, so the
	// row stays listed (the list is driven by `updateInfo.hasUpdate`, and checks
	// keep running) and only its rendering changes once the refreshed rows report
	// the new status back.
	async function handleToggleAutoUpdate(item: ContainerUpdateRow) {
		const enable = item.autoUpdateDisabled;
		togglingAutoUpdateIds = { ...togglingAutoUpdateIds, [rowKey(item)]: true };
		try {
			const operationResult = await tryCatch(containerService.setAutoUpdate(item.containerId, enable, item.environmentId));
			if (operationResult.error !== null) {
				toast.error(m.auto_update_failed(), { description: extractApiErrorMessage(operationResult.error) });
				return;
			}
			toast.success(enable ? m.auto_update_enabled_toast() : m.auto_update_disabled_toast());
			// The setting is saved at this point; a failed reload must not read as a failed toggle.
			const refreshResult = await tryCatch(
				(async () => {
					await onAutoUpdateChanged?.();
					await refreshRows();
				})()
			);
			if (refreshResult.error !== null) {
				toast.error(m.common_refresh_failed({ resource: m.updates() }), {
					description: extractApiErrorMessage(refreshResult.error)
				});
			}
		} finally {
			togglingAutoUpdateIds = { ...togglingAutoUpdateIds, [rowKey(item)]: false };
		}
	}

	function handleBulkUpdate(ids: string[]) {
		const updateScope = scope;
		const tracksActivities = environmentStore.shown.every((env) => hasPermission('activities:read', env.id));
		bulkConfirmAndRun({
			ids,
			title: m.updates_bulk_update_confirm_title({ count: ids.length }),
			message: m.updates_bulk_update_confirm_message({ count: ids.length }),
			confirmLabel: m.common_update(),
			run: (id, environmentId) => containerService.updateContainer(id, environmentId),
			messages: {
				success: (count) => m.containers_bulk_update_accepted({ count }),
				partial: (success, total, failed) => m.containers_bulk_update_accept_partial({ success, total, failed }),
				failure: () => m.containers_bulk_update_accept_failed()
			},
			acceptanceOnly: true,
			activityLink: hasShownPermission('activities:read'),
			onItemSuccess: (_id, activity) => {
				if (updateTableMounted) updateActivities.accept(activity);
			},
			setLoading: (loading) => (bulkUpdating = loading),
			onComplete: () => (updateTableMounted && updateScope === scope && !tracksActivities ? refreshRows() : undefined),
			clearSelection: () => (selectedIds = [])
		});
	}

	const bulkActions = $derived<BulkAction[]>(
		hasShownPermission('containers:autoupdate')
			? [
					{
						id: 'update',
						label: m.updates_bulk_update({ count: selectedIds.length }),
						action: 'update',
						onClick: handleBulkUpdate,
						loading: bulkUpdating,
						disabled: bulkUpdating,
						icon: UpdateIcon
					}
				]
			: []
	);
</script>

{#snippet NameCell({ item }: { item: ContainerUpdateRow })}
	<div class="flex items-center gap-2">
		<a
			class="font-medium hover:underline {item.autoUpdateDisabled ? 'text-muted-foreground' : ''}"
			href={environmentHref(`/containers/${item.containerId}`, item.environmentId)}
		>
			{item.name}
		</a>
		{#if item.autoUpdateDisabled}
			<Badge
				variant="gray"
				title={item.labelControlled
					? m.auto_update_controlled_by_label_checks_continue({ label: `${UPDATE_CHECK_LABEL}=false` })
					: m.auto_update_disabled_checks_continue()}
			>
				{m.updates_auto_update_disabled()}
			</Badge>
		{/if}
	</div>
{/snippet}

{#snippet ImageCell({ item }: { item: ContainerUpdateRow })}
	<!-- fallow-ignore-next-line code-duplication -- parallel container/project update tables; cells shared via components, thin column wrappers differ by row type -->
	<code class="text-xs">{item.imageRef}</code>
{/snippet}

{#snippet RowActions({ item }: { item: ContainerUpdateRow })}
	<IfPermitted perm="containers:autoupdate" envId={item.environmentId}>
		<RowActionsMenu>
			<DropdownMenu.Item onclick={() => handleUpdateContainer(item.container)} disabled={!!updatingContainerIds[rowKey(item)]}>
				{#if updatingContainerIds[rowKey(item)]}
					<Spinner class="size-4" />
				{:else}
					<UpdateIcon class="size-4" />
				{/if}
				{m.update_container()}
			</DropdownMenu.Item>

			<DropdownMenu.Item
				onclick={() => handleToggleAutoUpdate(item)}
				disabled={item.labelControlled || !item.statusAvailable || !!togglingAutoUpdateIds[rowKey(item)]}
				title={item.autoUpdateTitle}
			>
				{#if togglingAutoUpdateIds[rowKey(item)]}
					<Spinner class="size-4" />
				{:else if item.autoUpdateDisabled}
					<EyeOnIcon class="size-4" />
				{:else}
					<EyeOffIcon class="size-4" />
				{/if}
				{item.autoUpdateDisabled ? m.updates_enable_auto_update() : m.updates_disable_auto_update()}
			</DropdownMenu.Item>
		</RowActionsMenu>
	</IfPermitted>
{/snippet}

{#snippet ContainerUpdatesMobileCard({ item }: { item: ContainerUpdateRow })}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: ContainersIcon,
			variant: 'blue' as const
		})}
		title={(item: ContainerUpdateRow) => item.name}
		subtitle={(item: ContainerUpdateRow) => item.imageRef}
		badges={[
			(item: ContainerUpdateRow) =>
				item.autoUpdateDisabled ? { variant: 'gray' as const, text: m.updates_auto_update_disabled() } : null
		]}
		fields={[
			{
				label: m.common_current(),
				getValue: (item: ContainerUpdateRow) => item.currentValue
			},
			{
				label: m.image_update_latest_label(),
				getValue: (item: ContainerUpdateRow) => item.latestValue
			},
			{
				label: m.common_updated(),
				getValue: (item: ContainerUpdateRow) => formatImageUpdateCheckedAt(item.checkedAt)
			}
		]}
		rowActions={RowActions}
		onclick={(item: ContainerUpdateRow) => {
			window.location.href = environmentHref(`/containers/${item.containerId}`, item.environmentId);
		}}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-updates-container-table"
	items={tableItems}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRefresh={async (options) => {
		requestOptions = options;
		await onRefreshData(options as ContainerListRequestOptions);
		return tableItems;
	}}
	{columns}
	{mobileFields}
	{bulkActions}
	rowActions={RowActions}
	mobileCard={ContainerUpdatesMobileCard}
	withoutFilters
/>
