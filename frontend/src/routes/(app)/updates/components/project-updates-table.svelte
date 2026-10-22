<script lang="ts">
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
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { ProjectsIcon, ImagesIcon, UpdateIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { Project } from '#lib/types/swarm.js';
	import { bulkConfirmAndRun, confirmAndRun } from '#lib/utils/bulk-actions.js';
	import {
		environmentHref,
		hasShownPermission,
		rowKey,
		type EnvironmentPage,
		type EnvironmentRow
	} from '#lib/utils/environments.js';
	import { formatImageUpdateCheckedAt, formatImageUpdateValue } from '#lib/utils/image-updates.js';
	import { applyScopedUpdate, summarizeUpdateResult, throwOnUpdateFailure } from '#lib/utils/update-actions.js';

	type ProjectRow = EnvironmentRow<Project>;
	type ProjectUpdateRow = {
		id: string;
		environmentId: string;
		environmentName: string;
		environmentTagged: true;
		projectId: string;
		name: string;
		imageSummary: string;
		currentValue: string;
		latestValue: string;
		checkedAt: string;
		project: ProjectRow;
	};

	interface Props {
		projects: EnvironmentPage<Paginated<Project>>;
		requestOptions: SearchPaginationSortRequest;
		onRefreshData: (options: SearchPaginationSortRequest) => Promise<void>;
	}

	let { projects, requestOptions = $bindable(), onRefreshData }: Props = $props();

	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});
	let updatingProjectIds = $state<Record<string, boolean>>({});
	let bulkUpdating = $state(false);

	function mapProjectRow(project: ProjectRow): ProjectUpdateRow {
		const updateInfo = project.updateInfo;
		const updatedRefs = updateInfo?.updatedImageRefs ?? [];
		const summaryRefs = updatedRefs.length > 0 ? updatedRefs : (updateInfo?.imageRefs ?? []);
		const firstRef = summaryRefs[0] ?? '';

		let imageSummary = '-';
		if (summaryRefs.length === 1) {
			imageSummary = firstRef;
		} else if (summaryRefs.length > 1) {
			imageSummary = `${firstRef} +${summaryRefs.length - 1} more`;
		}

		// The list response already carries the per-reference result that raised the
		// notification, with the project's update policy applied, so the row must read
		// that record rather than the image-level check for the same reference.
		const info = updatedRefs.length === 1 ? updateInfo?.updateInfoByRef?.[firstRef] : undefined;
		// The backend blanks the target when services sharing a reference disagree, so
		// fall back to the multi-update summary instead of picking one arbitrarily.
		const hasSingleTarget = !!info && (!!info.latestVersion?.trim() || !!info.latestDigest?.trim());

		let currentValue = '-';
		let latestValue = '-';
		if (updatedRefs.length > 1 || (info && !hasSingleTarget)) {
			currentValue = m.images_has_updates();
			latestValue = m.images_has_updates();
		} else if (info) {
			currentValue = formatImageUpdateValue(info, 'current');
			latestValue = formatImageUpdateValue(info, 'latest');
		}

		return {
			id: project.id,
			environmentId: project.environmentId,
			environmentName: project.environmentName,
			environmentTagged: true,
			projectId: project.id,
			name: project.name,
			imageSummary,
			currentValue,
			latestValue,
			checkedAt: info?.checkTime || updateInfo?.lastCheckedAt || '',
			project
		};
	}

	const tableItems = $derived<Paginated<ProjectUpdateRow>>({
		...projects,
		data: (projects.data ?? []).map(mapProjectRow)
	});

	const columns = $derived([
		...environmentColumns<ProjectUpdateRow>(),
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'imageSummary', title: m.common_image(), sortable: false, cell: ImageCell },
		{ accessorKey: 'currentValue', title: m.common_current(), sortable: false, cellComponent: DigestCell },
		{ accessorKey: 'latestValue', title: m.image_update_latest_label(), sortable: false, cellComponent: DigestCell },
		{ accessorKey: 'checkedAt', title: m.common_updated(), sortable: false, cellComponent: CheckedAtCell }
	] satisfies ColumnSpec<ProjectUpdateRow>[]);

	const mobileFields = [
		{ id: 'imageSummary', label: m.common_image(), defaultVisible: true },
		{ id: 'currentValue', label: m.common_current(), defaultVisible: true },
		{ id: 'latestValue', label: m.image_update_latest_label(), defaultVisible: true },
		{ id: 'checkedAt', label: m.common_updated(), defaultVisible: true }
	];

	// A scoped updater run only touches the services whose images actually
	// changed, unlike a full project redeploy.
	function handleUpdateProject(item: ProjectUpdateRow) {
		confirmAndRun({
			title: m.common_update(),
			message: m.updates_update_confirm_message({ name: item.name }),
			confirmLabel: m.common_update(),
			setLoading: (loading) => {
				updatingProjectIds = { ...updatingProjectIds, [rowKey(item)]: loading };
			},
			run: () => applyScopedUpdate('project', item.projectId, item.environmentId),
			failureMessage: m.updates_apply_all_failed(),
			onSuccess: async (result) => {
				summarizeUpdateResult(result);
				await onRefreshData(requestOptions);
			}
		});
	}

	function handleBulkUpdate(ids: string[]) {
		bulkConfirmAndRun({
			ids,
			title: m.updates_bulk_update_confirm_title({ count: ids.length }),
			message: m.updates_bulk_update_confirm_message({ count: ids.length }),
			confirmLabel: m.common_update(),
			run: (id, environmentId) => applyScopedUpdate('project', id, environmentId).then(throwOnUpdateFailure),
			messages: {
				success: (count) => m.updates_bulk_update_success({ count }),
				partial: (success, total, failed) => m.updates_bulk_update_partial({ success, total, failed }),
				failure: () => m.updates_bulk_update_failed()
			},
			setLoading: (loading) => (bulkUpdating = loading),
			onComplete: () => onRefreshData(requestOptions),
			clearSelection: () => (selectedIds = []),
			sequential: true
		});
	}

	const bulkActions = $derived<BulkAction[]>(
		hasShownPermission(PERMISSIONS.IMAGE_UPDATES_CHECK)
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

{#snippet NameCell({ item }: { item: ProjectUpdateRow })}
	{#if item.project.isDiscovered}
		<span class="font-medium">{item.name}</span>
	{:else}
		<a class="font-medium hover:underline" href={environmentHref(`/projects/${item.projectId}`, item.environmentId)}>
			{item.name}
		</a>
	{/if}
{/snippet}

{#snippet ImageCell({ item }: { item: ProjectUpdateRow })}
	<div class="flex items-center gap-2">
		<ImagesIcon class="size-3.5 shrink-0 text-muted-foreground" />
		<span class="truncate text-sm" title={item.imageSummary !== '-' ? item.imageSummary : undefined}>
			{item.imageSummary}
		</span>
	</div>
{/snippet}

{#snippet RowActions({ item }: { item: ProjectUpdateRow })}
	<IfPermitted perm={PERMISSIONS.IMAGE_UPDATES_CHECK} envId={item.environmentId}>
		<RowActionsMenu>
			<DropdownMenu.Item onclick={() => handleUpdateProject(item)} disabled={!!updatingProjectIds[rowKey(item)]}>
				{#if updatingProjectIds[rowKey(item)]}
					<Spinner class="size-4" />
				{:else}
					<UpdateIcon class="size-4" />
				{/if}
				{m.common_update()}
			</DropdownMenu.Item>
		</RowActionsMenu>
	</IfPermitted>
{/snippet}

{#snippet ProjectUpdatesMobileCard({ item }: { item: ProjectUpdateRow })}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: ProjectsIcon,
			variant: 'amber' as const
		})}
		title={(item: ProjectUpdateRow) => item.name}
		subtitle={(item: ProjectUpdateRow) => item.imageSummary}
		fields={[
			{
				label: m.common_current(),
				getValue: (item: ProjectUpdateRow) => item.currentValue
			},
			{
				label: m.image_update_latest_label(),
				getValue: (item: ProjectUpdateRow) => item.latestValue
			},
			{
				label: m.common_updated(),
				getValue: (item: ProjectUpdateRow) => formatImageUpdateCheckedAt(item.checkedAt)
			}
		]}
		rowActions={RowActions}
		onclick={(item: ProjectUpdateRow) => {
			if (item.project.isDiscovered) return;
			window.location.href = environmentHref(`/projects/${item.projectId}`, item.environmentId);
		}}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-updates-project-table"
	items={tableItems}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRefresh={async (options) => {
		requestOptions = options;
		await onRefreshData(options);
		return tableItems;
	}}
	{columns}
	{mobileFields}
	{bulkActions}
	rowActions={RowActions}
	mobileCard={ProjectUpdatesMobileCard}
	withoutFilters
/>
