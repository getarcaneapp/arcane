<script lang="ts">
	import type { Project } from '$lib/types/project.type';
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { EditIcon, StartIcon, RestartIcon, StopIcon, TrashIcon, RedeployIcon, EllipsisIcon } from '$lib/icons';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import { getStatusVariant } from '$lib/utils/status.utils';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';
	import { format } from 'date-fns';
	import type { ColumnSpec, MobileFieldVisibility, BulkAction } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service';
	import { projectService } from '$lib/services/project-service';
	import { FolderOpenIcon, LayersIcon, CalendarIcon, ProjectsIcon, GitBranchIcon, RefreshIcon } from '$lib/icons';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import IconImage from '$lib/components/icon-image.svelte';
	import type { ActionStatus } from './projects-table.helpers';
	import { createProjectActions } from './projects-table.actions';
	import ProjectUpdateItem from '$lib/components/project-update-item.svelte';
	import {
		getProjectUpdateStatus,
		getProjectUpdateText,
		getProjectUpdateTooltip,
		getProjectUpdateVariant
	} from '$lib/utils/project-update.util';

	let {
		projects = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		withoutFilters = false,
		onRefreshData
	}: {
		projects: Paginated<Project>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		withoutFilters?: boolean;
		onRefreshData?: (options: SearchPaginationSortRequest) => Promise<void>;
	} = $props();

	let actionStatus = $state<Record<string, ActionStatus>>({});
	let checkingProjectIds = $state<Record<string, boolean>>({});

	let isBulkLoading = $state({
		up: false,
		down: false,
		redeploy: false
	});

	async function refreshProjects(options: SearchPaginationSortRequest = requestOptions) {
		if (onRefreshData) {
			await onRefreshData(options);
			return;
		}
		projects = await projectService.getProjects(options);
	}

	async function handleCheckProjectUpdates(project: Project) {
		const imageRefs = project.updateInfo?.imageRefs ?? [];
		if (imageRefs.length === 0 || checkingProjectIds[project.id]) {
			return;
		}

		checkingProjectIds = {
			...checkingProjectIds,
			[project.id]: true
		};

		try {
			const results = await imageService.checkMultipleImages(imageRefs);
			const firstError = Object.values(results)
				.find((result) => !!result?.error?.trim())
				?.error?.trim();
			const hasErrors = !!firstError;
			if (hasErrors) {
				toast.error(firstError || m.containers_check_updates_failed());
			} else {
				toast.success(m.images_update_check_completed());
			}
			await refreshProjects(requestOptions);
		} catch {
			toast.error(m.containers_check_updates_failed());
		} finally {
			checkingProjectIds = {
				...checkingProjectIds,
				[project.id]: false
			};
		}
	}

	function getStatusTooltip(project: Project): string | undefined {
		return project.status.toLowerCase() === 'unknown' && project.statusReason ? project.statusReason : undefined;
	}

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	const envId = $derived(environmentStore.selected?.id);

	const { performProjectAction, handleDestroyProject, handleSyncFromGit, handleBulkUp, handleBulkDown, handleBulkRedeploy } =
		createProjectActions({
			getRequestOptions: () => requestOptions,
			refreshProjects,
			setSelectedIds: (next) => {
				selectedIds = next;
			},
			actionStatus,
			isBulkLoading,
			getEnvId: () => envId
		});

	const isAnyLoading = $derived(
		Object.values(actionStatus).some((status) => status !== '') || Object.values(isBulkLoading).some((loading) => loading)
	);

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'path', title: m.common_working_directory(), sortable: true, cell: DirectoryCell },
		{ accessorKey: 'gitOpsManagedBy', title: m.projects_col_provider(), cell: ProviderCell },
		{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: StatusCell },
		{
			id: 'updates',
			accessorFn: (row) => getProjectUpdateStatus(row.updateInfo),
			title: m.containers_update_column(),
			sortable: false,
			cell: UpdatesCell
		},
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
		{ accessorKey: 'serviceCount', title: m.compose_services(), sortable: true }
	] satisfies ColumnSpec<Project>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'directory', label: m.common_working_directory(), defaultVisible: true },
		{ id: 'provider', label: m.projects_col_provider(), defaultVisible: true },
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'updates', label: m.containers_update_column(), defaultVisible: true },
		{ id: 'serviceCount', label: m.compose_services(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'up',
			label: m.projects_bulk_up({ count: selectedIds?.length ?? 0 }),
			action: 'up',
			onClick: handleBulkUp,
			loading: isBulkLoading.up,
			disabled: isAnyLoading,
			icon: StartIcon
		},
		{
			id: 'down',
			label: m.projects_bulk_down({ count: selectedIds?.length ?? 0 }),
			action: 'down',
			onClick: handleBulkDown,
			loading: isBulkLoading.down,
			disabled: isAnyLoading,
			icon: StopIcon
		},
		{
			id: 'redeploy',
			label: m.projects_bulk_redeploy({ count: selectedIds?.length ?? 0 }),
			action: 'redeploy',
			onClick: handleBulkRedeploy,
			loading: isBulkLoading.redeploy,
			disabled: isAnyLoading,
			icon: RedeployIcon
		}
	]);
</script>

{#snippet NameCell({ item }: { item: Project })}
	<div class="flex items-center gap-2">
		<IconImage src={item.iconUrl} alt={item.name} fallback={FolderOpenIcon} class="size-8" containerClass="size-10" />
		<a class="font-medium hover:underline" href="/projects/{item.id}">{item.name}</a>
	</div>
{/snippet}

{#snippet DirectoryCell({ item }: { item: Project })}
	<span class="text-muted-foreground block max-w-[22rem] truncate">{item.relativePath ?? item.dirName ?? item.path}</span>
{/snippet}

{#snippet ProviderCell({ item }: { item: Project })}
	<div class="flex items-center gap-2">
		{#if item.gitOpsManagedBy}
			<GitBranchIcon class="size-4" />
			<a class="font-medium hover:underline" href="/environments/{envId}/gitops">
				{m.projects_provider_git()}
			</a>
		{:else}
			<ProjectsIcon class="size-4" />
			<span>{m.projects_provider_local()}</span>
		{/if}
	</div>
{/snippet}

{#snippet ProviderField(value: { icon: any; text: string })}
	{@const Icon = value.icon}
	<span class="inline-flex items-center gap-2">
		<Icon class="size-3" />
		<span>{value.text}</span>
	</span>
{/snippet}

{#snippet StatusCell({ item }: { item: Project })}
	<StatusBadge
		variant={getStatusVariant(item.status)}
		text={capitalizeFirstLetter(item.status)}
		tooltip={getStatusTooltip(item)}
	/>
{/snippet}

{#snippet UpdatesCell({ item }: { item: Project })}
	<ProjectUpdateItem
		updateInfo={item.updateInfo}
		onCheck={() => handleCheckProjectUpdates(item)}
		checking={!!checkingProjectIds[item.id]}
		class="mr-2"
	/>
{/snippet}

{#snippet CreatedCell({ value }: { value: unknown })}
	{#if value}{format(new Date(String(value)), 'PP p')}{/if}
{/snippet}

{#snippet ProjectMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: Project;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={(item: Project) => ({
			component: FolderOpenIcon,
			variant: item.status === 'running' ? 'emerald' : item.status === 'exited' ? 'red' : 'amber',
			imageUrl: item.iconUrl,
			alt: item.name
		})}
		title={(item: Project) => item.name}
		subtitle={(item: Project) => ((mobileFieldVisibility['id'] ?? true) ? item.id : null)}
		badges={[
			(item: Project) =>
				(mobileFieldVisibility['status'] ?? true)
					? {
							variant: getStatusVariant(item.status),
							text: capitalizeFirstLetter(item.status),
							tooltip: getStatusTooltip(item)
						}
					: null,
			(item: Project) =>
				(mobileFieldVisibility['updates'] ?? true)
					? {
							variant: getProjectUpdateVariant(item.updateInfo),
							text: getProjectUpdateText(item.updateInfo),
							tooltip: getProjectUpdateTooltip(item.updateInfo)
						}
					: null
		]}
		fields={[
			{
				label: m.common_working_directory(),
				getValue: (item: Project) => item.relativePath ?? item.dirName ?? item.path,
				icon: FolderOpenIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['directory'] ?? true
			},
			{
				label: m.projects_col_provider(),
				type: 'component',
				getValue: (item: Project) => ({
					icon: item.gitOpsManagedBy ? GitBranchIcon : ProjectsIcon,
					text: item.gitOpsManagedBy ? m.projects_provider_git() : m.projects_provider_local()
				}),
				component: ProviderField,
				show: mobileFieldVisibility['provider'] ?? true
			},
			{
				label: m.compose_services(),
				getValue: (item: Project) => {
					const serviceCount = item.serviceCount ? Number(item.serviceCount) : (item.services?.length ?? 0);
					return `${serviceCount} ${Number(serviceCount) === 1 ? 'service' : 'services'}`;
				},
				icon: LayersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['serviceCount'] ?? true
			}
		]}
		footer={(mobileFieldVisibility['createdAt'] ?? true) && item.createdAt
			? {
					label: m.common_created(),
					getValue: (item: Project) => format(new Date(item.createdAt), 'PP p'),
					icon: CalendarIcon
				}
			: undefined}
		rowActions={RowActions}
		onclick={() => goto(`/projects/${item.id}`)}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: Project })}
	{@const status = actionStatus[item.id]}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="size-8">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon class="size-4" />
				</ArcaneButton>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => goto(`/projects/${item.id}`)} disabled={isAnyLoading}>
					<EditIcon class="size-4" />
					{m.common_edit()}
				</DropdownMenu.Item>

				{#if item.gitOpsManagedBy}
					<DropdownMenu.Item
						onclick={() => handleSyncFromGit(item.id, item.gitOpsManagedBy!)}
						disabled={status === 'syncing' || isAnyLoading}
					>
						{#if status === 'syncing'}
							<Spinner class="size-4" />
						{:else}
							<RefreshIcon class="size-4" />
						{/if}
						{m.git_sync_from_git()}
					</DropdownMenu.Item>
				{/if}

				<DropdownMenu.Separator />

				{#if item.status !== 'running'}
					<DropdownMenu.Item
						onclick={() => performProjectAction('start', item.id)}
						disabled={status === 'starting' || isAnyLoading}
					>
						{#if status === 'starting'}
							<Spinner class="size-4" />
						{:else}
							<StartIcon class="size-4" />
						{/if}
						{m.common_up()}
					</DropdownMenu.Item>
				{:else}
					<DropdownMenu.Item
						onclick={() => performProjectAction('stop', item.id)}
						disabled={status === 'stopping' || isAnyLoading}
					>
						{#if status === 'stopping'}
							<Spinner class="size-4" />
						{:else}
							<StopIcon class="size-4" />
						{/if}
						{m.common_down()}
					</DropdownMenu.Item>

					<DropdownMenu.Item
						onclick={() => performProjectAction('restart', item.id)}
						disabled={status === 'restarting' || isAnyLoading}
					>
						{#if status === 'restarting'}
							<Spinner class="size-4" />
						{:else}
							<RestartIcon class="size-4" />
						{/if}
						{m.common_restart()}
					</DropdownMenu.Item>
				{/if}

				<DropdownMenu.Item
					onclick={() => performProjectAction('redeploy', item.id)}
					disabled={status === 'redeploying' || isAnyLoading}
				>
					{#if status === 'redeploying'}
						<Spinner class="size-4" />
					{:else}
						<RedeployIcon class="size-4" />
					{/if}
					{m.compose_pull_redeploy()}
				</DropdownMenu.Item>

				<DropdownMenu.Separator />

				<DropdownMenu.Item
					variant="destructive"
					onclick={() => handleDestroyProject(item.id)}
					disabled={status === 'destroying' || isAnyLoading}
				>
					{#if status === 'destroying'}
						<Spinner class="size-4" />
					{:else}
						<TrashIcon class="size-4" />
					{/if}
					{m.compose_destroy()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-project-table"
	items={projects}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	{withoutFilters}
	onRefresh={async (options) => {
		requestOptions = options;
		await refreshProjects(options);
		return projects;
	}}
	{columns}
	{mobileFields}
	{bulkActions}
	rowActions={RowActions}
	mobileCard={ProjectMobileCardSnippet}
/>
