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
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { m } from '#lib/paraglide/messages';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared';
	import type { Project } from '#lib/types/swarm';
	import type { ImageUpdateInfoDto } from '#lib/types/docker';
	import { ProjectsIcon, ImagesIcon, UpdateIcon } from '#lib/icons';
	import { hasPermission } from '#lib/utils/auth';
	import { bulkConfirmAndRun, confirmAndRun } from '#lib/utils/bulk-actions';
	import { applyScopedUpdate, summarizeUpdateResult, throwOnUpdateFailure } from '#lib/utils/update-actions';
	import { formatImageUpdateCheckedAt, formatImageUpdateValue } from '#lib/utils/image-updates';

	type ProjectUpdateRow = {
		id: string;
		projectId: string;
		name: string;
		imageSummary: string;
		currentValue: string;
		latestValue: string;
		checkedAt: string;
		project: Project;
	};

	interface Props {
		projects: Paginated<Project>;
		requestOptions: SearchPaginationSortRequest;
		updateInfoByRef?: Record<string, ImageUpdateInfoDto>;
		onRefreshData: (options: SearchPaginationSortRequest) => Promise<void>;
	}

	let { projects = $bindable(), requestOptions = $bindable(), updateInfoByRef = {}, onRefreshData }: Props = $props();

	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});
	let updatingProjectIds = $state<Record<string, boolean>>({});
	let bulkUpdating = $state(false);

	function summarizeImageRefs(imageRefs: string[]): string {
		if (imageRefs.length === 0) return '-';
		if (imageRefs.length === 1) return imageRefs[0] ?? '-';
		return `${imageRefs[0] ?? ''} +${imageRefs.length - 1} more`;
	}

	function resolveProjectValue(project: Project, mode: 'current' | 'latest') {
		const updatedRefs = project.updateInfo?.updatedImageRefs ?? [];
		if (updatedRefs.length === 0) return '-';
		if (updatedRefs.length > 1) {
			return m.images_has_updates();
		}

		const firstRef = updatedRefs[0];
		const info = firstRef ? updateInfoByRef[firstRef] : undefined;
		if (!info) return '-';

		return formatImageUpdateValue(info, mode);
	}

	function resolveCheckedAt(project: Project) {
		const updatedRefs = project.updateInfo?.updatedImageRefs ?? [];
		if (updatedRefs.length === 1) {
			const firstRef = updatedRefs[0];
			return (firstRef ? updateInfoByRef[firstRef]?.checkTime : undefined) ?? project.updateInfo?.lastCheckedAt ?? '';
		}
		return project.updateInfo?.lastCheckedAt ?? '';
	}

	function mapProjectRow(project: Project): ProjectUpdateRow {
		const updatedRefs = project.updateInfo?.updatedImageRefs ?? project.updateInfo?.imageRefs ?? [];
		return {
			id: project.id,
			projectId: project.id,
			name: project.name,
			imageSummary: summarizeImageRefs(updatedRefs),
			currentValue: resolveProjectValue(project, 'current'),
			latestValue: resolveProjectValue(project, 'latest'),
			checkedAt: resolveCheckedAt(project),
			project
		};
	}

	const tableItems = $derived<Paginated<ProjectUpdateRow>>({
		...projects,
		data: (projects.data ?? []).map(mapProjectRow)
	});

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'imageSummary', title: m.common_image(), sortable: false, cell: ImageCell },
		{ accessorKey: 'currentValue', title: m.common_current(), sortable: false, cellComponent: DigestCell },
		{ accessorKey: 'latestValue', title: m.image_update_latest_digest_label(), sortable: false, cellComponent: DigestCell },
		{ accessorKey: 'checkedAt', title: m.common_updated(), sortable: false, cellComponent: CheckedAtCell }
	] satisfies ColumnSpec<ProjectUpdateRow>[];

	const mobileFields = [
		{ id: 'imageSummary', label: m.common_image(), defaultVisible: true },
		{ id: 'currentValue', label: m.common_current(), defaultVisible: true },
		{ id: 'latestValue', label: m.image_update_latest_digest_label(), defaultVisible: true },
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
				updatingProjectIds = { ...updatingProjectIds, [item.projectId]: loading };
			},
			run: () => applyScopedUpdate('project', item.projectId),
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
			run: (id) => applyScopedUpdate('project', id).then(throwOnUpdateFailure),
			messages: {
				success: (count) => m.updates_bulk_update_success({ count }),
				partial: (success, total, failed) => m.updates_bulk_update_partial({ success, total, failed }),
				failure: () => m.updates_bulk_update_failed()
			},
			setLoading: (loading) => (bulkUpdating = loading),
			onComplete: () => onRefreshData(requestOptions),
			clearSelection: () => (selectedIds = [])
		});
	}

	const bulkActions = $derived<BulkAction[]>(
		hasPermission('image-updates:check')
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
		<a class="font-medium hover:underline" href={`/projects/${item.projectId}`}>
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
	<IfPermitted perm="image-updates:check">
		<RowActionsMenu>
			<DropdownMenu.Item onclick={() => handleUpdateProject(item)} disabled={!!updatingProjectIds[item.projectId]}>
				{#if updatingProjectIds[item.projectId]}
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
				label: m.image_update_latest_digest_label(),
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
			window.location.href = `/projects/${item.projectId}`;
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
		return {
			...projects,
			data: (projects.data ?? []).map(mapProjectRow)
		};
	}}
	{columns}
	{mobileFields}
	{bulkActions}
	rowActions={RowActions}
	mobileCard={ProjectUpdatesMobileCard}
	withoutFilters
/>
