<script lang="ts">
	import { resolve } from '$app/paths';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

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
	import ResourceName from '#lib/components/resource-name.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { ContainersIcon, UpdateIcon, EyeOffIcon, EyeOnIcon, FileTextIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { containerService } from '#lib/services/container-service.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { PaginatedWithCounts } from '#lib/types/base.js';
	import type { ContainerPaginatedResponse, ContainerSummary } from '#lib/types/container.js';
	import type { ImageUpdateInfo } from '#lib/types/image.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import { cn } from '#lib/utils.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { bulkConfirmAndRun } from '#lib/utils/bulk-actions.js';
	import { confirmAndUpdateContainer } from '#lib/utils/container-actions.js';
	import { isAutoUpdateLabelDisabled, UPDATE_CHECK_LABEL } from '#lib/utils/container-auto-update.js';
	import { createContainerUpdateActivityTracker } from '#lib/utils/container-update-activities.js';
	import { hasShownPermission, listScopes, rowKey, type EnvironmentPage, type EnvironmentRow } from '#lib/utils/environments.js';
	import { formatImageUpdateCheckedAt, formatImageUpdateValue } from '#lib/utils/image-updates.js';
	import { quadletUnitHref } from '#lib/utils/podman.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import { getContainerDisplayName } from '../../containers/container-table.helpers';

	type ContainerRow = EnvironmentRow<ContainerSummary>;
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
		quadletHref?: string;
		/** False when the agent omitted `autoUpdateEnabled`; toggling is then unavailable. */
		statusAvailable: boolean;
		autoUpdateTitle: string;
		updateInfo?: ImageUpdateInfo;
		container: ContainerRow;
	};

	interface Props {
		containers: EnvironmentPage<ContainerPaginatedResponse>;
		requestOptions: SearchPaginationSortRequest;
		onRetry: () => Promise<unknown>;
		onAutoUpdateChanged?: () => Promise<unknown> | unknown;
	}

	let { containers, requestOptions = $bindable(), onRetry, onAutoUpdateChanged }: Props = $props();

	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});
	let updatingContainerIds = $state<Record<string, boolean>>({});
	let togglingAutoUpdateIds = $state<Record<string, boolean>>({});
	let bulkUpdating = $state(false);
	let releaseNotesTarget = $state<ContainerUpdateRow | null>(null);
	let releaseNotesOpen = $state(false);
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
				void queryClient.invalidateQueries({ queryKey: queryKeys.projects.listPrefix(listScope) });
			}
			for (const env of environmentStore.shown) {
				void queryClient.invalidateQueries({ queryKey: queryKeys.projects.environment(env.id) });
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
		const quadletHref = quadletUnitHref(container.labels, container.environmentId);
		const statusAvailable = typeof container.autoUpdateEnabled === 'boolean';
		const autoUpdateDisabled = container.autoUpdateEnabled === false;
		let autoUpdateTitle = m.updates_disable_auto_update_description();
		if (!statusAvailable) {
			autoUpdateTitle = m.auto_update_status_unavailable();
		} else if (quadletHref) {
			autoUpdateTitle = m.podman_quadlet_update_disabled();
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
			quadletHref,
			statusAvailable,
			autoUpdateTitle,
			updateInfo: container.updateInfo,
			container
		};
	}

	const tableItems = $derived<PaginatedWithCounts<ContainerUpdateRow, ContainerPaginatedResponse['counts']>>({
		...containers,
		data: (containers.data ?? []).map(mapContainerRow)
	});

	const columns = $derived([
		...environmentColumns<ContainerUpdateRow>(),
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'currentValue', id: 'version', title: m.common_version(), sortable: false, cell: VersionCell },
		{ accessorKey: 'imageRef', title: m.common_image(), hidden: true, sortable: true, cell: ImageCell },
		{ accessorKey: 'checkedAt', title: m.last_check(), sortable: false, cellComponent: CheckedAtCell }
	] satisfies ColumnSpec<ContainerUpdateRow>[]);

	const mobileFields = [
		{ id: 'currentValue', label: m.common_current(), defaultVisible: true },
		{ id: 'latestValue', label: m.image_update_latest_label(), defaultVisible: true },
		{ id: 'imageRef', label: m.common_image(), defaultVisible: true },
		{ id: 'checkedAt', label: m.last_check(), defaultVisible: true }
	];

	async function refreshRows() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.containers.listPrefix(scope) }, { throwOnError: true });
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

	// Disabling only updates the shared exclusion setting; the row stays listed.
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
		const tracksActivities = environmentStore.shown.every((env) => hasPermission('activities:list', env.id));
		bulkConfirmAndRun({
			ids,
			title: m.updates_bulk_update_confirm_title({ count: ids.length }),
			message: m.updates_bulk_update_confirm_message({ count: ids.length }),
			confirmLabel: m.common_update(),
			runInEnvironment: (id, environmentId) => containerService.updateContainer(id, environmentId),
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

	const canApplyUpdates = $derived(hasShownPermission('image-updates:apply'));

	const bulkActions = $derived<BulkAction[]>(
		canApplyUpdates
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

{#snippet NameCell({ item }: { item: ContainerUpdateRow })}
	<div class="flex min-w-0 items-center gap-2">
		<div class="min-w-0">
			<ResourceName
				name={item.name}
				href={resolve(`/(app)/containers/[containerId]?env=${encodeURIComponent(item.environmentId)}`, {
					containerId: item.containerId
				})}
				class={cn('font-medium', item.autoUpdateDisabled && 'text-muted-foreground')}
			>
				{#snippet tooltip()}
					<span class="block max-w-tooltip">
						<span class="block font-medium">{item.name}</span>
						<code class="block">{item.imageRef}</code>
					</span>
				{/snippet}
			</ResourceName>
			<code class="block font-mono text-xs whitespace-nowrap text-muted-foreground">{item.imageRef}</code>
		</div>
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
		{#if item.quadletHref}
			<Badge variant="purple" href={item.quadletHref} title={m.podman_quadlet_update_disabled()}>
				{m.podman_managed_by_quadlet()}
			</Badge>
		{/if}
	</div>
{/snippet}

{#snippet VersionCell({ item, view }: { item: ContainerUpdateRow; view: TableViewMode })}
	<VersionCellView current={item.currentValue} latest={item.latestValue} wrap={view === 'grid'} />
{/snippet}

{#snippet ImageCell({ item }: { item: ContainerUpdateRow })}
	<code class="block font-mono text-xs whitespace-nowrap">{item.imageRef}</code>
{/snippet}

{#snippet RowActions({ item }: { item: ContainerUpdateRow })}
	<IfPermitted perm={['image-updates:read', 'image-updates:apply', 'containers:autoupdate']} envId={item.environmentId}>
		<RowActionsMenu name={item.name}>
			{#if item.updateInfo?.hasUpdate}
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
				<DropdownMenu.Item onclick={() => handleUpdateContainer(item.container)} disabled={!!updatingContainerIds[rowKey(item)]}>
					{#if updatingContainerIds[rowKey(item)]}
						<Spinner class="size-4" />
					{:else}
						<UpdateIcon class="size-4" />
					{/if}
					{m.update_container()}
				</DropdownMenu.Item>
			</IfPermitted>

			<IfPermitted perm="containers:autoupdate" envId={item.environmentId}>
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
			</IfPermitted>
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
				label: m.last_check(),
				getValue: (item: ContainerUpdateRow) => formatImageUpdateCheckedAt(item.checkedAt)
			}
		]}
		rowActions={RowActions}
		href={(item: ContainerUpdateRow) =>
			resolve(`/(app)/containers/[containerId]?env=${encodeURIComponent(item.environmentId)}`, {
				containerId: item.containerId
			})}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-updates-container-table"
	items={tableItems}
	bind:requestOptions
	bind:selectedIds
	selectionDisabled={!canApplyUpdates}
	bind:mobileFieldVisibility
	{onRetry}
	{columns}
	{mobileFields}
	{bulkActions}
	rowActions={RowActions}
	mobileCard={ContainerUpdatesMobileCard}
	withoutFilters
/>

{#if releaseNotesTarget}
	<ImageReleaseNotesDialog
		bind:open={releaseNotesOpen}
		environmentId={releaseNotesTarget.environmentId}
		imageRef={releaseNotesTarget.imageRef}
		version={releaseNotesTarget.updateInfo?.updateType === 'tag' ? releaseNotesTarget.updateInfo.latestVersion : undefined}
		digest={releaseNotesTarget.updateInfo?.latestDigest}
	/>
{/if}
