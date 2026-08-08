<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import {
		UniversalMobileCard,
		type BulkAction,
		type ColumnSpec,
		type MobileFieldVisibility
	} from '#lib/components/arcane-table';
	import DigestCell from '#lib/components/arcane-table/cells/digest-cell.svelte';
	import CheckedAtCell from '#lib/components/arcane-table/cells/checked-at-cell.svelte';
	import { Badge } from '#lib/components/ui/badge';
	import { m } from '#lib/paraglide/messages';
	import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared';
	import type { ContainerSummaryDto } from '#lib/types/docker';
	import type { ImageUpdateInfoDto } from '#lib/types/docker';
	import { containerService } from '#lib/services/container-service';
	import type { ContainersPaginatedResponse, ContainerListRequestOptions } from '#lib/services/container-service';
	import { ContainersIcon, UpdateIcon, EyeOffIcon, EyeOnIcon } from '#lib/icons';
	import { getContainerDisplayName } from '../containers/container-table.helpers';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { hasPermission } from '#lib/utils/auth';
	import { confirmAndUpdateContainer } from '#lib/utils/container-actions';
	import { isAutoUpdateIgnored, isAutoUpdateLabelDisabled } from '#lib/utils/container-auto-update';
	import { confirmAndRun } from '#lib/utils/bulk-actions';
	import { summarizeUpdateResult } from '#lib/utils/update-actions';
	import { imageService } from '#lib/services/image-service';
	import { formatImageUpdateCheckedAt, formatImageUpdateValue } from '#lib/utils/image-updates';
	import { toast } from 'svelte-sonner';

	type ContainerUpdateRow = {
		id: string;
		containerId: string;
		name: string;
		imageRef: string;
		currentValue: string;
		latestValue: string;
		checkedAt: string;
		ignored: boolean;
		labelControlled: boolean;
		updateInfo?: ImageUpdateInfoDto;
		container: ContainerSummaryDto;
	};

	interface Props {
		containers: ContainersPaginatedResponse;
		requestOptions: SearchPaginationSortRequest;
		/** `autoUpdateExcludedContainers` setting — a CSV of ignored container names. */
		excludedContainers?: string;
		onRefreshData: (options: ContainerListRequestOptions) => Promise<ContainersPaginatedResponse>;
		onIgnoreChanged?: () => Promise<unknown> | unknown;
	}

	let {
		containers = $bindable(),
		requestOptions = $bindable(),
		excludedContainers,
		onRefreshData,
		onIgnoreChanged
	}: Props = $props();

	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});
	let updatingContainerIds = $state<Record<string, boolean>>({});
	let ignoringContainerIds = $state<Record<string, boolean>>({});
	let bulkUpdating = $state(false);

	function mapContainerRow(container: ContainerSummaryDto): ContainerUpdateRow {
		const name = getContainerDisplayName(container);
		return {
			id: container.id,
			containerId: container.id,
			name,
			imageRef: container.image,
			currentValue: formatImageUpdateValue(container.updateInfo, 'current'),
			latestValue: formatImageUpdateValue(container.updateInfo, 'latest'),
			checkedAt: container.updateInfo?.checkTime ?? '',
			ignored: isAutoUpdateIgnored(name, container.labels, excludedContainers),
			labelControlled: isAutoUpdateLabelDisabled(container.labels),
			updateInfo: container.updateInfo,
			container
		};
	}

	const tableItems = $derived<Paginated<ContainerUpdateRow, ContainersPaginatedResponse['counts']>>({
		...containers,
		data: (containers.data ?? []).map(mapContainerRow)
	});

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'imageRef', title: m.common_image(), sortable: true, cell: ImageCell },
		{ accessorKey: 'currentValue', title: m.common_current(), sortable: false, cell: DigestCol },
		{ accessorKey: 'latestValue', title: m.image_update_latest_digest_label(), sortable: false, cell: DigestCol },
		{ accessorKey: 'checkedAt', title: m.common_updated(), sortable: false, cell: CheckedAtCol }
	] satisfies ColumnSpec<ContainerUpdateRow>[];

	const mobileFields = [
		{ id: 'imageRef', label: m.common_image(), defaultVisible: true },
		{ id: 'currentValue', label: m.common_current(), defaultVisible: true },
		{ id: 'latestValue', label: m.image_update_latest_digest_label(), defaultVisible: true },
		{ id: 'checkedAt', label: m.common_updated(), defaultVisible: true }
	];

	async function refreshRows() {
		containers = await onRefreshData(requestOptions as ContainerListRequestOptions);
	}

	async function handleUpdateContainer(container: ContainerSummaryDto) {
		const containerName = getContainerDisplayName(container);

		confirmAndUpdateContainer({
			containerId: container.id,
			containerName,
			showPullingToast: true,
			setLoading: (loading) => {
				updatingContainerIds = { ...updatingContainerIds, [container.id]: loading };
			},
			onRefresh: refreshRows
		});
	}

	// Ignoring writes to the shared `autoUpdateExcludedContainers` setting, so the
	// row stays listed (the list is driven by `updateInfo.hasUpdate`) and only its
	// rendering changes once the settings query reports the new value back.
	async function handleToggleIgnore(item: ContainerUpdateRow) {
		const enable = item.ignored;
		ignoringContainerIds = { ...ignoringContainerIds, [item.containerId]: true };
		try {
			await containerService.setAutoUpdate(item.containerId, enable);
			toast.success(enable ? m.auto_update_enabled_toast() : m.auto_update_disabled_toast());
			await onIgnoreChanged?.();
		} catch (error) {
			console.error('Auto-update toggle failed:', error);
			toast.error(m.auto_update_failed());
		} finally {
			ignoringContainerIds = { ...ignoringContainerIds, [item.containerId]: false };
		}
	}

	// One scoped updater run for the whole selection: the backend deduplicates
	// swarm task replicas down to a single service update per owning service and
	// reports the merged tally back.
	function handleBulkUpdate(ids: string[]) {
		confirmAndRun({
			title: m.updates_bulk_update_confirm_title({ count: ids.length }),
			message: m.updates_bulk_update_confirm_message({ count: ids.length }),
			confirmLabel: m.common_update(),
			setLoading: (loading) => (bulkUpdating = loading),
			run: () => imageService.runAutoUpdate({ type: 'container', resourceIds: ids }),
			failureMessage: m.updates_bulk_update_failed(),
			onSuccess: async (result) => {
				summarizeUpdateResult(result);
				selectedIds = [];
				await refreshRows();
			}
		});
	}

	const bulkActions = $derived<BulkAction[]>(
		hasPermission('containers:autoupdate')
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
		<a class="font-medium hover:underline {item.ignored ? 'text-muted-foreground' : ''}" href={`/containers/${item.containerId}`}>
			{item.name}
		</a>
		{#if item.ignored}
			<Badge
				variant="outline"
				class="text-muted-foreground"
				title={item.labelControlled ? m.auto_update_controlled_by_label() : undefined}
			>
				{m.common_ignored()}
			</Badge>
		{/if}
	</div>
{/snippet}

{#snippet ImageCell({ item }: { item: ContainerUpdateRow })}
	<!-- fallow-ignore-next-line code-duplication -- parallel container/project update tables; cells shared via components, thin column wrappers differ by row type -->
	<code class="text-xs">{item.imageRef}</code>
{/snippet}

{#snippet DigestCol({ value }: { value: unknown })}
	<DigestCell {value} />
{/snippet}

{#snippet CheckedAtCol({ value }: { value: unknown })}
	<CheckedAtCell {value} />
{/snippet}

{#snippet RowActions({ item }: { item: ContainerUpdateRow })}
	<IfPermitted perm="containers:autoupdate">
		<RowActionsMenu>
			<DropdownMenu.Item
				onclick={() => handleUpdateContainer(item.container)}
				disabled={!!updatingContainerIds[item.containerId]}
			>
				{#if updatingContainerIds[item.containerId]}
					<Spinner class="size-4" />
				{:else}
					<UpdateIcon class="size-4" />
				{/if}
				{m.update_container()}
			</DropdownMenu.Item>

			<DropdownMenu.Item
				onclick={() => handleToggleIgnore(item)}
				disabled={item.labelControlled || !!ignoringContainerIds[item.containerId]}
				title={item.labelControlled ? m.auto_update_controlled_by_label() : m.updates_ignore_description()}
			>
				{#if ignoringContainerIds[item.containerId]}
					<Spinner class="size-4" />
				{:else if item.ignored}
					<EyeOnIcon class="size-4" />
				{:else}
					<EyeOffIcon class="size-4" />
				{/if}
				{item.ignored ? m.common_unignore() : m.common_ignore()}
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
		badges={[(item: ContainerUpdateRow) => (item.ignored ? { variant: 'gray' as const, text: m.common_ignored() } : null)]}
		fields={[
			{
				label: m.common_current(),
				getValue: (item: ContainerUpdateRow) => item.currentValue
			},
			{
				label: m.image_update_latest_digest_label(),
				getValue: (item: ContainerUpdateRow) => item.latestValue
			},
			{
				label: m.common_updated(),
				getValue: (item: ContainerUpdateRow) => formatImageUpdateCheckedAt(item.checkedAt)
			}
		]}
		rowActions={RowActions}
		onclick={(item: ContainerUpdateRow) => {
			window.location.href = `/containers/${item.containerId}`;
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
		const next = await onRefreshData(options as ContainerListRequestOptions);
		containers = next;
		return {
			...next,
			data: (next.data ?? []).map(mapContainerRow)
		};
	}}
	{columns}
	{mobileFields}
	{bulkActions}
	rowActions={RowActions}
	mobileCard={ContainerUpdatesMobileCard}
	withoutFilters
/>
