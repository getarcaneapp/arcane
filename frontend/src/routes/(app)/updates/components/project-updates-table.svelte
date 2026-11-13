<script lang="ts">
	import { resolve } from '$app/paths';
	import { useQueryClient } from '@tanstack/svelte-query';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import type { TableViewMode } from '#lib/components/arcane-table/arcane-table.types.svelte';
	import CheckedAtCell from '#lib/components/arcane-table/cells/checked-at-cell.svelte';
	import VersionCellView from '#lib/components/arcane-table/cells/version-cell.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import {
		UniversalMobileCard,
		type BulkAction,
		type ColumnSpec,
		type MobileFieldVisibility
	} from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import ImageReleaseNotesDialog from '#lib/components/dialogs/image-release-notes-dialog.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { ProjectsIcon, ImagesIcon, UpdateIcon, FileTextIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { Paginated } from '#lib/types/base.js';
	import type { ImageUpdateResponse } from '#lib/types/imageupdate.js';
	import type { Project } from '#lib/types/project.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import { bulkConfirmAndRun, confirmAndRun } from '#lib/utils/bulk-actions.js';
	import { hasShownPermission, rowKey, type EnvironmentPage, type EnvironmentRow } from '#lib/utils/environments.js';
	import { blockedUpdateTitle, formatImageUpdateCheckedAt, formatImageUpdateValue } from '#lib/utils/image-updates.js';
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
		releaseNotesRef: string;
		releaseNotesInfo?: ImageUpdateResponse;
		blockedTitle: string | null;
		project: ProjectRow;
	};

	interface Props {
		projects: EnvironmentPage<Paginated<Project>>;
		requestOptions: SearchPaginationSortRequest;
		onRetry: () => Promise<unknown>;
	}

	let { projects, requestOptions = $bindable(), onRetry }: Props = $props();

	const queryClient = useQueryClient();

	async function refreshRows() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.projects.listPrefix(environmentStore.shownKey) });
	}

	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});
	let updatingProjectIds = $state<Record<string, boolean>>({});
	let bulkUpdating = $state(false);
	let releaseNotesTarget = $state<ProjectUpdateRow | null>(null);
	let releaseNotesOpen = $state(false);

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

		// Read the per-reference result from the list response, not the image-level check.
		const info = updatedRefs.length === 1 ? updateInfo?.updateInfoByRef?.[firstRef] : undefined;
		// A blank target means services sharing the reference disagree.
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
			releaseNotesRef: hasSingleTarget ? firstRef : '',
			releaseNotesInfo: info,
			blockedTitle:
				updatedRefs.map((ref) => blockedUpdateTitle(updateInfo?.updateInfoByRef?.[ref])).find((title) => title !== null) ?? null,
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
		{ accessorKey: 'currentValue', id: 'version', title: m.common_version(), sortable: false, cell: VersionCell },
		{ accessorKey: 'imageSummary', title: m.common_image(), sortable: false, cell: ImageCell },
		{ accessorKey: 'checkedAt', title: m.last_check(), sortable: false, cellComponent: CheckedAtCell }
	] satisfies ColumnSpec<ProjectUpdateRow>[]);

	const mobileFields = [
		{ id: 'currentValue', label: m.common_current(), defaultVisible: true },
		{ id: 'latestValue', label: m.image_update_latest_label(), defaultVisible: true },
		{ id: 'imageSummary', label: m.common_image(), defaultVisible: true },
		{ id: 'checkedAt', label: m.last_check(), defaultVisible: true }
	];

	// A scoped updater run only touches services whose images changed.
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
				await refreshRows();
			}
		});
	}

	function handleBulkUpdate(ids: string[]) {
		bulkConfirmAndRun({
			ids,
			title: m.updates_bulk_update_confirm_title({ count: ids.length }),
			message: m.updates_bulk_update_confirm_message({ count: ids.length }),
			confirmLabel: m.common_update(),
			runInEnvironment: (id, environmentId) => applyScopedUpdate('project', id, environmentId).then(throwOnUpdateFailure),
			messages: {
				success: (count) => m.updates_bulk_update_success({ count }),
				partial: (success, total, failed) => m.updates_bulk_update_partial({ success, total, failed }),
				failure: () => m.updates_bulk_update_failed()
			},
			setLoading: (loading) => (bulkUpdating = loading),
			onComplete: () => refreshRows(),
			clearSelection: () => (selectedIds = []),
			sequential: true
		});
	}

	const bulkActions = $derived<BulkAction[]>(
		hasShownPermission('image-updates:apply')
			? [
					{
						id: 'update',
						label: m.updates_bulk_update({ count: selectedIds.length }),
						action: 'update',
						onclick: handleBulkUpdate,
						loading: bulkUpdating,
						disabled: bulkUpdating,
						icon: UpdateIcon
					}
				]
			: []
	);
</script>

{#snippet VersionCell({ item, view }: { item: ProjectUpdateRow; view: TableViewMode })}
	<VersionCellView current={item.currentValue} latest={item.latestValue} wrap={view === 'grid'} />
{/snippet}

{#snippet NameCell({ item }: { item: ProjectUpdateRow })}
	<div class="flex min-w-0 items-center gap-2">
		{#if item.project.isDiscovered}
			<span class="block max-w-resource-name truncate font-medium" title={item.name}>{item.name}</span>
		{:else}
			<a
				class="block max-w-resource-name truncate font-medium hover:underline"
				href={resolve(`/(app)/projects/[projectId]?env=${encodeURIComponent(item.environmentId)}`, { projectId: item.projectId })}
				title={item.name}
			>
				{item.name}
			</a>
		{/if}
		{#if item.blockedTitle}
			<Badge variant="amber" title={item.blockedTitle}>
				{m.blocked()}
			</Badge>
		{/if}
	</div>
{/snippet}

{#snippet ImageCell({ item }: { item: ProjectUpdateRow })}
	<div class="flex items-center gap-2">
		<ImagesIcon class="size-3.5 shrink-0 text-muted-foreground" />
		<span class="max-w-image-reference truncate text-sm" title={item.imageSummary !== '-' ? item.imageSummary : undefined}>
			{item.imageSummary}
		</span>
	</div>
{/snippet}

{#snippet RowActions({ item }: { item: ProjectUpdateRow })}
	<IfPermitted perm={['image-updates:read', 'image-updates:apply']} envId={item.environmentId}>
		<RowActionsMenu name={item.name}>
			{#if item.releaseNotesRef}
				<IfPermitted perm="image-updates:read" envId={item.environmentId}>
					<DropdownMenu.Item
						onclick={() => {
							releaseNotesTarget = item;
							releaseNotesOpen = true;
						}}
					>
						<FileTextIcon class="size-4" />
						{m.image_update_release_notes_button()}
					</DropdownMenu.Item>
				</IfPermitted>
			{/if}

			<IfPermitted perm="image-updates:apply" envId={item.environmentId}>
				<DropdownMenu.Item onclick={() => handleUpdateProject(item)} disabled={!!updatingProjectIds[rowKey(item)]}>
					{#if updatingProjectIds[rowKey(item)]}
						<Spinner class="size-4" />
					{:else}
						<UpdateIcon class="size-4" />
					{/if}
					{m.common_update()}
				</DropdownMenu.Item>
			</IfPermitted>
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
		badges={[(item: ProjectUpdateRow) => (item.blockedTitle ? { variant: 'amber' as const, text: m.blocked() } : null)]}
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
				label: m.last_check(),
				getValue: (item: ProjectUpdateRow) => formatImageUpdateCheckedAt(item.checkedAt)
			}
		]}
		rowActions={RowActions}
		href={(item: ProjectUpdateRow) =>
			item.project.isDiscovered
				? undefined
				: resolve(`/(app)/projects/[projectId]?env=${encodeURIComponent(item.environmentId)}`, { projectId: item.projectId })}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-updates-project-table"
	items={tableItems}
	bind:requestOptions
	bind:selectedIds
	selectionDisabled={!hasShownPermission('image-updates:apply')}
	bind:mobileFieldVisibility
	{onRetry}
	{columns}
	{mobileFields}
	{bulkActions}
	rowActions={RowActions}
	mobileCard={ProjectUpdatesMobileCard}
	withoutFilters
/>

{#if releaseNotesTarget}
	<ImageReleaseNotesDialog
		bind:open={releaseNotesOpen}
		environmentId={releaseNotesTarget.environmentId}
		imageRef={releaseNotesTarget.releaseNotesRef}
		version={releaseNotesTarget.releaseNotesInfo?.updateType === 'tag'
			? releaseNotesTarget.releaseNotesInfo.latestVersion
			: undefined}
		digest={releaseNotesTarget.releaseNotesInfo?.latestDigest}
	/>
{/if}
