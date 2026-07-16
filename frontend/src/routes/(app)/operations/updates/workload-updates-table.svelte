<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { UniversalMobileCard, type ColumnSpec, type MobileFieldVisibility } from '$lib/components/arcane-table';
	import CheckedAtCell from '$lib/components/arcane-table/cells/checked-at-cell.svelte';
	import DigestCell from '$lib/components/arcane-table/cells/digest-cell.svelte';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { ContainersIcon, ProjectsIcon, UpdateIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import type { ContainersPaginatedResponse } from '$lib/services/container-service';
	import type { ImageUpdateInfoDto } from '$lib/types/docker';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
	import type { Project } from '$lib/types/swarm';
	import { formatImageUpdateCheckedAt, formatImageUpdateValue } from '$lib/utils/image-updates';
	import { getContainerDisplayName } from '../../containers/container-table.helpers';

	type WorkloadKind = 'project' | 'container';

	type WorkloadUpdateRow = {
		id: string;
		resourceId: string;
		kind: WorkloadKind;
		kindLabel: string;
		name: string;
		environmentName: string;
		affectedServices: string;
		imageRefs: string[];
		imageSummary: string;
		currentValue: string;
		latestValue: string;
		checkedAt: string;
		project?: Project;
	};

	type WorkloadUpdateLists = {
		containers: ContainersPaginatedResponse;
		projects: Paginated<Project>;
	};

	interface Props extends WorkloadUpdateLists {
		requestOptions: SearchPaginationSortRequest;
		updateInfoByRef?: Record<string, ImageUpdateInfoDto>;
		onRefreshData: (options: SearchPaginationSortRequest) => Promise<WorkloadUpdateLists>;
		environmentName: string;
		onUpdate: (type: WorkloadKind, resourceId: string) => Promise<void>;
	}

	let {
		containers,
		projects,
		requestOptions = $bindable(),
		updateInfoByRef = {},
		environmentName,
		onRefreshData,
		onUpdate
	}: Props = $props();

	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});
	let updatingWorkloadIds = $state<Record<string, boolean>>({});

	function workloadKindLabel(kind: WorkloadKind) {
		return kind === 'project' ? m.project() : m.container();
	}

	function summarizeImageRefs(imageRefs: string[]): string {
		if (imageRefs.length === 0) return '-';
		if (imageRefs.length === 1) return imageRefs[0] ?? '-';
		return `${imageRefs[0] ?? ''} +${imageRefs.length - 1}`;
	}

	function resolveProjectValue(project: Project, mode: 'current' | 'latest') {
		const updatedRefs = project.updateInfo?.updatedImageRefs ?? [];
		if (updatedRefs.length === 0) return '-';
		if (updatedRefs.length > 1) return m.images_has_updates();

		const firstRef = updatedRefs[0];
		const info = firstRef ? updateInfoByRef[firstRef] : undefined;
		return info ? formatImageUpdateValue(info, mode) : '-';
	}

	function resolveProjectCheckedAt(project: Project) {
		const updatedRefs = project.updateInfo?.updatedImageRefs ?? [];
		if (updatedRefs.length === 1) {
			const firstRef = updatedRefs[0];
			return (firstRef ? updateInfoByRef[firstRef]?.checkTime : undefined) ?? project.updateInfo?.lastCheckedAt ?? '';
		}
		return project.updateInfo?.lastCheckedAt ?? '';
	}

	function mapWorkloadUpdates(lists: WorkloadUpdateLists, options: SearchPaginationSortRequest): Paginated<WorkloadUpdateRow> {
		const rows: WorkloadUpdateRow[] = [];
		for (const container of lists.containers.data ?? []) {
			const imageRefs = [container.image];
			rows.push({
				id: `container:${container.id}`,
				resourceId: container.id,
				kind: 'container',
				kindLabel: workloadKindLabel('container'),
				name: getContainerDisplayName(container),
				environmentName,
				affectedServices: '-',
				imageRefs,
				imageSummary: summarizeImageRefs(imageRefs),
				currentValue: formatImageUpdateValue(container.updateInfo, 'current'),
				latestValue: formatImageUpdateValue(container.updateInfo, 'latest'),
				checkedAt: container.updateInfo?.checkTime ?? ''
			});
		}
		for (const project of lists.projects.data ?? []) {
			const imageRefs = project.updateInfo?.updatedImageRefs ?? project.updateInfo?.imageRefs ?? [];
			rows.push({
				id: `project:${project.id}`,
				resourceId: project.id,
				kind: 'project',
				kindLabel: workloadKindLabel('project'),
				name: project.name,
				environmentName,
				affectedServices: project.serviceCount,
				imageRefs,
				imageSummary: summarizeImageRefs(imageRefs),
				currentValue: resolveProjectValue(project, 'current'),
				latestValue: resolveProjectValue(project, 'latest'),
				checkedAt: resolveProjectCheckedAt(project),
				project
			});
		}

		const direction = options.sort?.direction === 'desc' ? -1 : 1;
		rows.sort((left, right) => {
			const byName = left.name.localeCompare(right.name) * direction;
			return byName || left.id.localeCompare(right.id);
		});

		const currentPage = options.pagination?.page ?? 1;
		const itemsPerPage = options.pagination?.limit ?? 20;
		const totalItems =
			(lists.containers.pagination?.totalItems ?? lists.containers.data.length) +
			(lists.projects.pagination?.totalItems ?? lists.projects.data.length);
		const start = (currentPage - 1) * itemsPerPage;

		return {
			data: rows.slice(start, start + itemsPerPage),
			pagination: {
				currentPage,
				itemsPerPage,
				totalItems,
				totalPages: totalItems === 0 ? 0 : Math.ceil(totalItems / itemsPerPage)
			}
		};
	}

	const tableItems = $derived(mapWorkloadUpdates({ containers, projects }, requestOptions));

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'kindLabel', title: m.common_type(), sortable: false, cell: KindCell },
		{ accessorKey: 'environmentName', title: m.environments_title(), sortable: false },
		{ accessorKey: 'affectedServices', title: m.compose_services(), sortable: false },
		{ accessorKey: 'imageSummary', title: m.common_image(), sortable: false, cell: ImageCell },
		{ accessorKey: 'currentValue', title: m.image_update_current_label(), sortable: false, cell: DigestCol },
		{ accessorKey: 'latestValue', title: m.image_update_latest_digest_label(), sortable: false, cell: DigestCol },
		{ accessorKey: 'checkedAt', title: m.common_updated(), sortable: false, cell: CheckedAtCol },
		{ id: 'actions', title: m.common_actions(), sortable: false, cell: ActionsCell }
	] satisfies ColumnSpec<WorkloadUpdateRow>[];

	const mobileFields = [
		{ id: 'kindLabel', label: m.common_type(), defaultVisible: true },
		{ id: 'imageSummary', label: m.common_image(), defaultVisible: true },
		{ id: 'currentValue', label: m.image_update_current_label(), defaultVisible: true },
		{ id: 'latestValue', label: m.image_update_latest_digest_label(), defaultVisible: true },
		{ id: 'checkedAt', label: m.common_updated(), defaultVisible: true }
	];

	function handleUpdateWorkload(item: WorkloadUpdateRow) {
		openConfirmDialog({
			title: m.operations_update_workload_title({ name: item.name }),
			message: m.operations_update_workload_description({ name: item.name }),
			confirm: {
				label: m.common_update(),
				action: async () => {
					updatingWorkloadIds = { ...updatingWorkloadIds, [item.id]: true };
					try {
						await onUpdate(item.kind, item.resourceId);
					} finally {
						updatingWorkloadIds = { ...updatingWorkloadIds, [item.id]: false };
					}
				}
			}
		});
	}
</script>

{#snippet NameCell({ item }: { item: WorkloadUpdateRow })}
	{#if item.kind === 'project' && item.project?.isDiscovered}
		<span class="font-medium">{item.name}</span>
	{:else}
		<a
			class="font-medium hover:underline"
			href={item.kind === 'project' ? `/projects/${item.resourceId}` : `/containers/${item.resourceId}`}
		>
			{item.name}
		</a>
	{/if}
{/snippet}

{#snippet KindCell({ item }: { item: WorkloadUpdateRow })}
	<div class="flex items-center gap-2">
		{#if item.kind === 'project'}
			<ProjectsIcon class="size-4" />
		{:else}
			<ContainersIcon class="size-4" />
		{/if}
		<span>{item.kindLabel}</span>
	</div>
{/snippet}

{#snippet ImageCell({ item }: { item: WorkloadUpdateRow })}
	<code class="block max-w-80 truncate text-xs text-muted-foreground" title={item.imageRefs.join(', ')}>
		{item.imageSummary}
	</code>
{/snippet}

{#snippet DigestCol({ value }: { value: unknown })}
	<DigestCell {value} />
{/snippet}

{#snippet CheckedAtCol({ value }: { value: unknown })}
	<CheckedAtCell {value} />
{/snippet}

{#snippet ActionsCell({ item }: { item: WorkloadUpdateRow })}
	<IfPermitted perm="image-updates:check">
		<ArcaneButton
			action="update"
			tone="outline"
			size="sm"
			showLabel={false}
			class="size-7 border-transparent bg-transparent p-0 shadow-none hover:bg-primary/10 hover:text-primary"
			onclick={() => handleUpdateWorkload(item)}
			loading={!!updatingWorkloadIds[item.id]}
			disabled={!!updatingWorkloadIds[item.id]}
			icon={UpdateIcon}
			title={m.common_update()}
		/>
	</IfPermitted>
{/snippet}

{#snippet WorkloadUpdatesMobileCard({ item }: { item: WorkloadUpdateRow })}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: item.kind === 'project' ? ProjectsIcon : ContainersIcon,
			variant: item.kind === 'project' ? ('amber' as const) : ('blue' as const)
		})}
		title={(row: WorkloadUpdateRow) => row.name}
		subtitle={(row: WorkloadUpdateRow) => row.kindLabel}
		fields={[
			{
				label: m.common_image(),
				getValue: (row: WorkloadUpdateRow) => row.imageSummary
			},
			{
				label: m.image_update_current_label(),
				getValue: (row: WorkloadUpdateRow) => row.currentValue
			},
			{
				label: m.image_update_latest_digest_label(),
				getValue: (row: WorkloadUpdateRow) => row.latestValue
			},
			{
				label: m.common_updated(),
				getValue: (row: WorkloadUpdateRow) => formatImageUpdateCheckedAt(row.checkedAt)
			}
		]}
		onclick={(row: WorkloadUpdateRow) => {
			if (row.kind === 'project' && row.project?.isDiscovered) return;
			window.location.href = row.kind === 'project' ? `/projects/${row.resourceId}` : `/containers/${row.resourceId}`;
		}}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-updates-workload-table"
	items={tableItems}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRefresh={async (options) => {
		requestOptions = options;
		return mapWorkloadUpdates(await onRefreshData(options), options);
	}}
	{columns}
	{mobileFields}
	mobileCard={WorkloadUpdatesMobileCard}
	withoutFilters
	selectionDisabled
/>
