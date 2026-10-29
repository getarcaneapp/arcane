<script lang="ts">
	import { mode } from 'mode-watcher';
	import type { Snippet } from 'svelte';
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import ActionMenuItem from '#lib/components/arcane-table/cells/action-menu-item.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import type { ColumnSpec, MobileFieldVisibility, BulkAction } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import IconImage from '#lib/components/icon-image.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import ProjectTagEditor from '#lib/components/project-tag-editor.svelte';
	import ProjectUpdateItem from '#lib/components/project-update-item.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { AlertIcon, BoxIcon, EditIcon, StartIcon, RestartIcon, StopIcon, TrashIcon, RedeployIcon } from '#lib/icons/index.js';
	import { FolderOpenIcon, LayersIcon, CalendarIcon, ProjectsIcon, GitBranchIcon, RefreshIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { projectService } from '#lib/services/project-service.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { Project, ProjectTagColor, ProjectTagOption } from '#lib/types/swarm.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { goto } from '#lib/utils/base-path.js';
	import { hasAnyLoadingState } from '#lib/utils/bulk-actions.js';
	import { getStatusVariant, getThemedIconUrl } from '#lib/utils/docker.js';
	import {
		getProjectUpdateStatus,
		getProjectUpdateText,
		getProjectUpdateTooltip,
		getProjectUpdateVariant
	} from '#lib/utils/docker.js';
	import {
		environmentHref,
		type EnvironmentPage,
		type EnvironmentRow,
		fetchAcrossEnvironments,
		hasShownPermission,
		readableEnvironments,
		rowKey
	} from '#lib/utils/environments.js';
	import { capitalizeFirstLetter, formatDateTimeShort } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { summarizeUpdateCheckResult } from '#lib/utils/update-actions.js';

	import { createProjectActions } from '../projects-table.actions';
	import type { ActionStatus } from '../projects-table.helpers';

	type ProjectRow = EnvironmentRow<Project>;

	let {
		projects = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		withoutFilters = false,
		showArchived = false,
		onToggleArchived,
		onRefreshData,
		availableTags = [],
		emptyState
	}: {
		projects: EnvironmentPage<Paginated<Project>>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		withoutFilters?: boolean;
		showArchived?: boolean;
		onToggleArchived?: (checked: boolean) => void | Promise<void>;
		onRefreshData?: (options: SearchPaginationSortRequest) => Promise<void>;
		availableTags?: ProjectTagOption[];
		emptyState?: Snippet;
	} = $props();

	let actionStatus = $state<Record<string, ActionStatus>>({});
	let checkingProjectIds = $state<Record<string, boolean>>({});
	let tagCatalog = $derived(availableTags);

	let isBulkLoading = $state({
		up: false,
		down: false,
		redeploy: false,
		archive: false
	});

	async function refreshProjects(options: SearchPaginationSortRequest = requestOptions) {
		if (onRefreshData) {
			await onRefreshData(options);
			return;
		}
		projects = await fetchAcrossEnvironments(readableEnvironments('projects:list'), options, (id, request, config) =>
			projectService.getProjectsForEnvironment(id, request, config)
		);
	}

	async function handleCheckProjectUpdates(project: ProjectRow) {
		if (checkingProjectIds[rowKey(project)]) {
			return;
		}

		checkingProjectIds = {
			...checkingProjectIds,
			[rowKey(project)]: true
		};

		try {
			const operationResult = await tryCatch(
				(async () => {
					const result = await projectService.checkUpdates(project.id, project.environmentId);
					summarizeUpdateCheckResult(result);
					await refreshProjects(requestOptions);
				})()
			);
			if (operationResult.error !== null) {
				toast.error(m.containers_check_updates_failed());
			}
		} finally {
			checkingProjectIds = {
				...checkingProjectIds,
				[rowKey(project)]: false
			};
		}
	}

	async function handleTagToggle(project: ProjectRow, name: string, attached: boolean, color: ProjectTagColor) {
		const response = await projectService.updateProjectTag(project.id, name, attached, color, project.environmentId);
		if (attached && !tagCatalog.some((tag) => tag.name === name)) {
			tagCatalog = [...tagCatalog, { name, color }].sort((a, b) => a.name.localeCompare(b.name));
		}
		projects = {
			...projects,
			data: projects.data.map((item) => (rowKey(item) === rowKey(project) ? { ...item, tags: response.tags } : item))
		};
		return response.tags;
	}

	function getConfigurationErrorMessage(project: Project): string | undefined {
		const configurationError = project.configurationError;
		if (!configurationError) return undefined;
		const params = { path: configurationError.path, uid: configurationError.uid, gid: configurationError.gid };
		return configurationError.blocksOperations ? m.env_file_unreadable_blocked(params) : m.env_file_unreadable_ignored(params);
	}

	function getStatusTooltip(project: Project): string | undefined {
		const configurationMessage = getConfigurationErrorMessage(project);
		if (configurationMessage) return configurationMessage;
		return project.status.toLowerCase() === 'unknown' && project.statusReason ? project.statusReason : undefined;
	}

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	const canDeployProject = $derived(hasShownPermission('projects:deploy'));
	const canDownProject = $derived(hasShownPermission('projects:down'));
	const canArchiveProject = $derived(hasShownPermission('projects:archive'));

	const {
		performProjectAction,
		handleDestroyProject,
		handleSyncFromGit,
		handleBulkUp,
		handleBulkDown,
		handleBulkRedeploy,
		handleBulkArchive
	} = createProjectActions({
		getRequestOptions: () => requestOptions,
		refreshProjects,
		setSelectedIds: (next) => {
			selectedIds = next;
		},
		actionStatus,
		isBulkLoading
	});

	const isAnyLoading = $derived(hasAnyLoadingState(actionStatus, isBulkLoading));
	const selectedProjects = $derived.by(() => (projects?.data ?? []).filter((project) => selectedIds?.includes(rowKey(project))));
	const hasRedeployDisabledSelection = $derived.by(() => selectedProjects.some((project) => project.redeployDisabled));
	const hasArchivedSelection = $derived.by(() => selectedProjects.some((project) => project.isArchived));
	const isProjectArchiveBlocked = (project: Project) =>
		Number(project.runningCount) > 0 ||
		project.status === 'running' ||
		project.status === 'partially running' ||
		project.status === 'deploying' ||
		project.status === 'restarting';
	const hasRunningSelection = $derived.by(() => selectedProjects.some((project) => isProjectArchiveBlocked(project)));
	const hasConfigurationBlockedSelection = $derived.by(() =>
		selectedProjects.some((project) => project.configurationError?.blocksOperations)
	);

	const columns = $derived.by(
		() =>
			[
				...environmentColumns<ProjectRow>(),
				{ accessorKey: 'id', title: m.common_id(), hidden: true },
				{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
				{
					id: 'tags',
					accessorFn: (row) => row.tags?.map((tag) => tag.name) ?? [],
					title: m.common_tags(),
					cell: TagsCell,
					filterOptions: tagCatalog.map((tag) => ({ label: tag.name, value: tag.name }))
				},
				{ accessorKey: 'path', title: m.common_working_directory(), sortable: true, cell: DirectoryCell },
				{ accessorKey: 'gitOpsManagedBy', title: m.projects_col_provider(), cell: ProviderCell },
				{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: StatusCell },
				{
					id: 'updates',
					accessorFn: (row) => getProjectUpdateStatus(row.updateInfo),
					title: m.updates(),
					sortable: false,
					cell: UpdatesCell
				},
				{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
				{ accessorKey: 'serviceCount', title: m.services(), sortable: true },
				{ id: 'label', accessorFn: () => '', title: m.common_labels(), hidden: true }
			] satisfies ColumnSpec<ProjectRow>[]
	);

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'directory', label: m.common_working_directory(), defaultVisible: true },
		{ id: 'provider', label: m.projects_col_provider(), defaultVisible: true },
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'tags', label: m.common_tags(), defaultVisible: true },
		{ id: 'updates', label: m.updates(), defaultVisible: true },
		{ id: 'serviceCount', label: m.services(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'up',
			label: m.projects_bulk_up({ count: selectedIds?.length ?? 0 }),
			action: 'up',
			onClick: handleBulkUp,
			loading: isBulkLoading.up,
			disabled: !canDeployProject || isAnyLoading || hasArchivedSelection || hasConfigurationBlockedSelection,
			disabledReason: hasConfigurationBlockedSelection
				? m.env_file_unreadable_title()
				: hasArchivedSelection
					? m.projects_archived_badge()
					: undefined,
			icon: StartIcon
		},
		{
			id: 'down',
			label: m.projects_bulk_down({ count: selectedIds?.length ?? 0 }),
			action: 'down',
			onClick: handleBulkDown,
			loading: isBulkLoading.down,
			disabled: !canDownProject || isAnyLoading || hasArchivedSelection || hasConfigurationBlockedSelection,
			disabledReason: hasConfigurationBlockedSelection
				? m.env_file_unreadable_title()
				: hasArchivedSelection
					? m.projects_archived_badge()
					: undefined,
			icon: StopIcon
		},
		{
			id: 'redeploy',
			label: m.projects_bulk_redeploy({ count: selectedIds?.length ?? 0 }),
			action: 'redeploy',
			onClick: handleBulkRedeploy,
			loading: isBulkLoading.redeploy,
			disabled:
				!canDeployProject ||
				isAnyLoading ||
				hasRedeployDisabledSelection ||
				hasArchivedSelection ||
				hasConfigurationBlockedSelection,
			disabledReason: hasConfigurationBlockedSelection
				? m.env_file_unreadable_title()
				: hasArchivedSelection
					? m.projects_archived_badge()
					: hasRedeployDisabledSelection
						? m.common_redeploy_disabled_arcane_self()
						: undefined,
			icon: RedeployIcon
		},
		{
			id: 'archive',
			label: m.projects_bulk_archive({ count: selectedIds?.length ?? 0 }),
			action: 'archive',
			onClick: handleBulkArchive,
			loading: isBulkLoading.archive,
			disabled: !canArchiveProject || isAnyLoading || hasArchivedSelection || hasRunningSelection,
			disabledReason: hasRunningSelection
				? m.projects_archive_requires_stopped()
				: hasArchivedSelection
					? m.projects_archived_badge()
					: undefined
		}
	]);
</script>

{#snippet NameCell({ item }: { item: ProjectRow })}
	<div class="flex items-center gap-2">
		<IconImage
			src={getThemedIconUrl(item, mode.current)}
			alt={item.name}
			fallback={FolderOpenIcon}
			class="size-6"
			containerClass="size-8"
		/>
		<a class="font-medium hover:underline" href={environmentHref(`/projects/${item.id}`, item.environmentId)}>{item.name}</a>
		{#if item.isArchived}
			<span class="rounded bg-muted px-1.5 py-0.5 text-xs font-medium text-muted-foreground">{m.projects_archived_badge()}</span>
		{/if}
	</div>
{/snippet}

{#snippet TagsCell({ item }: { item: ProjectRow })}
	<ProjectTagEditor
		tags={item.tags ?? []}
		availableTags={tagCatalog}
		canEdit={hasPermission('projects:update', item.environmentId) && !item.isDiscovered}
		onToggle={(name, attached, color) => handleTagToggle(item, name, attached, color)}
	/>
{/snippet}

{#snippet DirectoryCell({ item }: { item: ProjectRow })}
	<span class="block max-w-88 truncate text-muted-foreground">{item.relativePath ?? item.dirName ?? item.path}</span>
{/snippet}

{#snippet ProviderCell({ item }: { item: ProjectRow })}
	<div class="flex items-center gap-2">
		{#if item.gitOpsManagedBy}
			<GitBranchIcon class="size-4" />
			<a class="font-medium hover:underline" href={environmentHref('/gitops', item.environmentId)}>
				{m.git()}
			</a>
		{:else}
			<ProjectsIcon class="size-4" />
			<span>{m.local()}</span>
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

{#snippet StatusCell({ item }: { item: ProjectRow })}
	{@const statusTooltip = getStatusTooltip(item)}
	{#if actionStatus[rowKey(item)] === 'redeploying'}
		<Badge variant="primary" minWidth="20" beam>{m.common_action_redeploying()}</Badge>
	{:else if statusTooltip}
		<ArcaneTooltip.Root>
			<ArcaneTooltip.Trigger>
				<span class="inline-flex items-center gap-1.5">
					<Badge variant={getStatusVariant(item.status)} minWidth="20">{capitalizeFirstLetter(item.status)}</Badge>
					{#if item.configurationError}
						<AlertIcon class="size-4 text-warning" aria-label={m.env_file_unreadable_title()} />
					{/if}
				</span>
			</ArcaneTooltip.Trigger>
			<ArcaneTooltip.Content>
				<p class="max-w-xs text-xs">{statusTooltip}</p>
			</ArcaneTooltip.Content>
		</ArcaneTooltip.Root>
	{:else}
		<Badge variant={getStatusVariant(item.status)} minWidth="20">{capitalizeFirstLetter(item.status)}</Badge>
	{/if}
{/snippet}

{#snippet UpdatesCell({ item }: { item: ProjectRow })}
	<ProjectUpdateItem
		updateInfo={item.updateInfo}
		environmentId={item.environmentId}
		onCheck={() => handleCheckProjectUpdates(item)}
		checking={!!checkingProjectIds[rowKey(item)]}
		disabled={!!item.isArchived || !!item.configurationError?.blocksOperations}
		class="mr-2"
	/>
{/snippet}

{#snippet CreatedCell({ value }: { value: unknown })}
	{#if value}{formatDateTimeShort(String(value))}{/if}
{/snippet}

{#snippet ProjectMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: ProjectRow;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={(item: ProjectRow) => ({
			component: FolderOpenIcon,
			variant: item.status === 'running' ? 'emerald' : item.status === 'exited' ? 'red' : 'amber',
			imageUrl: getThemedIconUrl(item, mode.current) ?? undefined,
			alt: item.name
		})}
		title={(item: ProjectRow) => item.name}
		subtitle={(item: ProjectRow) => ((mobileFieldVisibility['id'] ?? true) ? item.id : null)}
		badges={[
			(item: ProjectRow) =>
				(mobileFieldVisibility['status'] ?? true)
					? {
							variant: getStatusVariant(item.status),
							text: capitalizeFirstLetter(item.status),
							tooltip: getStatusTooltip(item)
						}
					: null,
			(item: ProjectRow) =>
				item.isArchived
					? {
							variant: 'gray' as const,
							text: m.projects_archived_badge()
						}
					: null,
			(item: ProjectRow) =>
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
				label: m.common_tags(),
				type: 'component',
				getValue: (item: ProjectRow) => ({ item }),
				component: TagsCell,
				show: mobileFieldVisibility['tags'] ?? true
			},
			{
				label: m.common_working_directory(),
				getValue: (item: ProjectRow) => item.relativePath ?? item.dirName ?? item.path,
				icon: FolderOpenIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['directory'] ?? true
			},
			{
				label: m.projects_col_provider(),
				type: 'component',
				getValue: (item: ProjectRow) => ({
					icon: item.gitOpsManagedBy ? GitBranchIcon : ProjectsIcon,
					text: item.gitOpsManagedBy ? m.git() : m.local()
				}),
				component: ProviderField,
				show: mobileFieldVisibility['provider'] ?? true
			},
			{
				label: m.services(),
				getValue: (item: ProjectRow) => {
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
					getValue: (item: ProjectRow) => formatDateTimeShort(item.createdAt),
					icon: CalendarIcon
				}
			: undefined}
		rowActions={RowActions}
		onclick={() => goto(environmentHref(`/projects/${item.id}`, item.environmentId))}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: ProjectRow })}
	{@const status = actionStatus[rowKey(item)]}
	{@const envId = item.environmentId}
	{@const configurationBlocked = !!item.configurationError?.blocksOperations}
	{@const lifecycleDisabled = item.isArchived || isAnyLoading || configurationBlocked}
	{@const archivedTitle = configurationBlocked
		? m.env_file_unreadable_title()
		: item.isArchived
			? m.projects_archived_badge()
			: undefined}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => goto(environmentHref(`/projects/${item.id}`, envId))} disabled={isAnyLoading}>
			<EditIcon class="size-4" />
			{m.common_edit()}
		</DropdownMenu.Item>

		{#if item.gitOpsManagedBy && hasPermission('projects:update', envId)}
			<ActionMenuItem
				onclick={() => handleSyncFromGit(item.id, item.gitOpsManagedBy!, envId)}
				disabled={isAnyLoading}
				icon={RefreshIcon}
				label={m.git_sync_from_git()}
				loading={status === 'syncing'}
			/>
		{/if}

		<DropdownMenu.Separator />

		{#if item.status !== 'running'}
			<IfPermitted perm="projects:deploy" {envId}>
				<ActionMenuItem
					onclick={() => performProjectAction('start', item.id, envId)}
					disabled={lifecycleDisabled}
					title={archivedTitle}
					icon={StartIcon}
					label={m.common_up()}
					loading={status === 'starting'}
				/>
			</IfPermitted>
		{:else}
			<IfPermitted perm="projects:down" {envId}>
				<ActionMenuItem
					onclick={() => performProjectAction('stop', item.id, envId)}
					disabled={lifecycleDisabled}
					title={archivedTitle}
					icon={StopIcon}
					label={m.common_down()}
					loading={status === 'stopping'}
				/>
			</IfPermitted>

			<IfPermitted perm="projects:restart" {envId}>
				<ActionMenuItem
					onclick={() => performProjectAction('restart', item.id, envId)}
					disabled={lifecycleDisabled}
					title={archivedTitle}
					icon={RestartIcon}
					label={m.common_restart()}
					loading={status === 'restarting'}
				/>
			</IfPermitted>
		{/if}

		<IfPermitted perm="projects:deploy" {envId}>
			{#if item.redeployDisabled}
				<DropdownMenu.Item disabled title={m.common_redeploy_disabled_arcane_self()}>
					<RedeployIcon class="size-4 opacity-50" />
					{m.compose_pull_redeploy()}
				</DropdownMenu.Item>
			{:else}
				<ActionMenuItem
					onclick={() => performProjectAction('redeploy', item.id, envId)}
					disabled={lifecycleDisabled}
					title={archivedTitle}
					icon={RedeployIcon}
					label={m.compose_pull_redeploy()}
					loading={status === 'redeploying'}
				/>
			{/if}
		</IfPermitted>

		<DropdownMenu.Separator />

		<IfPermitted perm="projects:archive" {envId}>
			{#if item.isArchived}
				<ActionMenuItem
					onclick={() => performProjectAction('unarchive', item.id, envId)}
					disabled={isAnyLoading}
					icon={BoxIcon}
					label={m.projects_unarchive()}
					loading={status === 'unarchiving'}
				/>
			{:else}
				<ActionMenuItem
					onclick={() => performProjectAction('archive', item.id, envId)}
					disabled={isProjectArchiveBlocked(item) || isAnyLoading}
					title={isProjectArchiveBlocked(item) ? m.projects_archive_requires_stopped() : undefined}
					icon={BoxIcon}
					label={m.projects_archive()}
					loading={status === 'archiving'}
				/>
			{/if}
		</IfPermitted>

		<IfPermitted perm="projects:delete" {envId}>
			<ActionMenuItem
				destructive
				onclick={() => handleDestroyProject(item.id, envId)}
				disabled={isAnyLoading}
				icon={TrashIcon}
				label={m.compose_destroy()}
				loading={status === 'destroying'}
			/>
		</IfPermitted>
	</RowActionsMenu>
{/snippet}

{#snippet ArchivedToolbarAction()}
	<div
		class="flex min-h-9 w-full items-center gap-2 rounded-md border border-border/60 bg-muted/30 px-2.5 py-1.5 md:w-auto md:border-0 md:bg-transparent md:px-1 md:py-0"
	>
		<Switch
			id="projects-show-archived"
			checked={showArchived}
			onCheckedChange={(checked) => {
				void onToggleArchived?.(checked === true);
			}}
		/>
		<Label for="projects-show-archived" variant="muted" class="mb-0 min-w-0">
			{m.projects_show_archived()}
		</Label>
	</div>
{/snippet}

<ArcaneTable
	{emptyState}
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
	customToolbarActions={ArchivedToolbarAction}
	rowActions={RowActions}
	mobileCard={ProjectMobileCardSnippet}
/>
