<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import { EmptyState } from '#lib/components/states/index.js';
	import { displaySize, FileDropZone, type FileDropZoneProps } from '#lib/components/ui/file-drop-zone/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Progress } from '#lib/components/ui/progress/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { TABLE_PAGE_SIZE_ALL } from '#lib/constants/table-pagination.js';
	import { CloseIcon, ContainersIcon, ProjectsIcon, PlayIcon, StopIcon, UploadIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { projectListQuery, projectStatusCountsQuery } from '#lib/query/projects.js';
	import { projectTagsQuery } from '#lib/query/projects.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { imageService } from '#lib/services/image-service.js';
	import { projectService } from '#lib/services/project-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { ProjectStatusCounts } from '#lib/types/project.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import type { ChunkedUploadProgress } from '#lib/types/ui/upload.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { pinViewAction } from '#lib/utils/dashboard-layout.js';
	import {
		acrossEnvironmentsNote,
		actionEnvironments,
		destinationEnvironmentId,
		hasShownPermission,
		listScopes,
		readableEnvironments,
		settleAcrossEnvironments,
		withFailedEnvironments
	} from '#lib/utils/environments.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import ProjectsTable from './components/projects-table.svelte';

	let { data } = $props();

	function withArchivedFilter(options: SearchPaginationSortRequest, show: boolean): SearchPaginationSortRequest {
		const filters = { ...options.filters };
		if (show) {
			filters['archived'] = 'true';
		} else {
			delete filters['archived'];
		}

		return {
			...options,
			filters: Object.keys(filters).length > 0 ? filters : undefined
		};
	}

	let baseProjectRequestOptions = $state(untrack(() => withArchivedFilter(data.projectRequestOptions, data.showArchived)));
	let selectedIds = $state<string[]>([]);
	let isManualRefreshing = $state(false);
	const queryClient = useQueryClient();
	const scope = $derived(environmentStore.shownKey);
	const showArchived = $derived(page.url.searchParams.get('archived') === 'true');
	const projectRequestOptions = $derived(withArchivedFilter(baseProjectRequestOptions, showArchived));
	const countsFallback: ProjectStatusCounts = {
		runningProjects: 0,
		stoppedProjects: 0,
		totalProjects: 0,
		archivedProjects: 0,
		updatesAvailable: 0
	};

	// The load already cached the first page under its key; seeding every key with it would hide other pages and filters.
	const projectsQuery = createQuery(() => {
		const queryScope = scope;
		return {
			...projectListQuery(queryScope, readableEnvironments('projects:list'), projectRequestOptions),
			placeholderData: (previous, query) => {
				if (query?.queryKey[1] === queryScope) return previous;
				return undefined;
			},
			select: (value) => ({ envId: queryScope, value }),
			refetchOnMount: false
		};
	});
	let projects = $derived(projectsQuery.data?.envId === scope ? projectsQuery.data.value : null);

	const statusCountsQuery = createQuery(() => {
		const queryScope = scope;
		return {
			...projectStatusCountsQuery(queryScope, readableEnvironments('projects:list')),
			select: (value) => ({ envId: queryScope, value }),
			refetchOnMount: false
		};
	});
	const tagsQuery = createQuery(() => {
		const queryScope = scope;
		const environments = readableEnvironments('projects:list');
		return {
			...projectTagsQuery(queryScope, environments),
			select: (value) => ({ envId: queryScope, value }),
			refetchOnMount: false
		};
	});
	const projectTags = $derived(tagsQuery.data?.envId === scope ? tagsQuery.data.value.tags : []);
	const relatedFailures = $derived(
		[statusCountsQuery.data, tagsQuery.data].flatMap((result) => (result?.envId === scope ? result.value.failedEnvironments : []))
	);
	const resourcesReady = $derived(projects !== null);
	// Tags change rarely, so a refresh reloads them only after an environment failed to return them.
	const refetchFailedTags = () =>
		tagsQuery.isError || tagsQuery.data?.value.failedEnvironments.length ? tagsQuery.refetch() : undefined;

	async function updateEnvironmentProjects(requestedEnvId: string) {
		// Refresh image updates, then redeploy only projects the list reports as outdated.
		const imageCheckResults = await imageService.checkAllImages(requestedEnvId);

		// The updates filter is evaluated over every candidate in one pass, so a single unbounded request is cheapest.
		const projectsWithUpdates = await projectService.getProjectsForEnvironment(requestedEnvId, {
			filters: { updates: 'has_update' },
			pagination: { page: 1, limit: TABLE_PAGE_SIZE_ALL }
		});
		// Untracked compose rows cannot be deployed by ID.
		const projectsToUpdate = projectsWithUpdates.data.filter((p) => !p.isDiscovered);

		if (projectsToUpdate.length === 0) {
			return { updated: 0, activityId: extractActivityId(imageCheckResults) };
		}

		const results = await Promise.allSettled(
			projectsToUpdate.map(async (proj) => {
				// pullPolicy always already pulls fresh images.
				await projectService.deployProject(proj.id, 'up', { pullPolicy: 'always' }, requestedEnvId);
				return proj.name;
			})
		);
		const failed = results.filter((r): r is PromiseRejectedResult => r.status === 'rejected');
		if (failed.length > 0) {
			const succeeded = results.length - failed.length;
			throw new Error(`${failed.length} project(s) failed to update (${succeeded} succeeded)`);
		}

		return { updated: results.length, activityId: extractActivityId(imageCheckResults) };
	}

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: queryKeys.projects.checkUpdates(scope),
		mutationFn: async (_requestedScope: string) => {
			const results = await settleAcrossEnvironments(
				actionEnvironments('image-updates:check', 'projects:deploy'),
				updateEnvironmentProjects
			);
			return {
				updated: results.reduce((total, result) => total + result.value.updated, 0),
				activityId: results[0]?.value.activityId,
				environmentIds: results.map(({ environment }) => environment.id)
			};
		},
		onSuccess: async (result, requestedScope) => {
			const toastOptions = activityToastOptions(result.activityId);
			if (result && result.updated === 0) {
				toast.success(m.image_update_up_to_date_title(), toastOptions);
			} else {
				toast.success(m.compose_update_success(), toastOptions);
			}
			await Promise.all(
				[...new Set([requestedScope, ...listScopes(result.environmentIds)])].map((listScope) =>
					queryClient.invalidateQueries({ queryKey: queryKeys.projects.listPrefix(listScope) })
				)
			);
		},
		onError: (error, requestedScope) => {
			toast.error(error.message || m.containers_check_updates_failed());
			void queryClient.invalidateQueries({ queryKey: queryKeys.projects.listPrefix(requestedScope) });
		}
	}));

	const projectStatusCounts = $derived(statusCountsQuery.data?.envId === scope ? statusCountsQuery.data.value : countsFallback);
	const totalCompose = $derived(projectStatusCounts.totalProjects);
	const runningCompose = $derived(projectStatusCounts.runningProjects);
	const stoppedCompose = $derived(projectStatusCounts.stoppedProjects);
	const archivedCompose = $derived(projectStatusCounts.archivedProjects);
	const isRefreshBlocked = $derived(isManualRefreshing || projectsQuery.isFetching || statusCountsQuery.isFetching);

	function handleCheckForUpdates() {
		openConfirmDialog({
			title: m.compose_update_projects(),
			message:
				`${m.compose_update_projects_confirm_message()} ${acrossEnvironmentsNote(actionEnvironments('image-updates:check', 'projects:deploy'))}`.trim(),
			confirm: {
				label: m.compose_update_projects(),
				destructive: false,
				action: () => checkUpdatesMutation.mutate(scope)
			}
		});
	}

	async function refreshCompose() {
		if (isRefreshBlocked) return;
		const requestedScope = scope;
		isManualRefreshing = true;
		try {
			await Promise.all([projectsQuery.refetch(), statusCountsQuery.refetch(), refetchFailedTags()]);
		} finally {
			if (requestedScope === scope) {
				isManualRefreshing = false;
			}
		}
	}

	async function toggleArchived(next: boolean) {
		const url = new URL(page.url.href);
		if (next) {
			url.searchParams.set('archived', 'true');
		} else {
			url.searchParams.delete('archived');
		}
		await goto(`${resolve('/(app)/projects')}${url.searchParams.size > 0 ? `?${url.searchParams}` : ''}`, {
			reset: false,
			replace: true
		});
	}

	const canCreateProject = $derived(hasShownPermission('projects:create'));
	const canDeployProject = $derived(hasShownPermission('projects:deploy'));

	let importOpen = $state(false);
	let importFile = $state<File | null>(null);
	let importNameOverride = $state<string | null>(null);
	let importStart = $state(false);
	let importing = $state(false);
	let importProgress = $state<ChunkedUploadProgress | null>(null);
	let pickedImportEnvironmentId = $state('');
	const importEnvironmentId = $derived(destinationEnvironmentId(pickedImportEnvironmentId, 'projects:create'));
	const canStartImport = $derived(hasPermission('projects:deploy', importEnvironmentId));
	const importName = $derived(importNameOverride ?? importFile?.name.replace(/(\.arcane)?\.(tar\.gz|tgz)$/i, '') ?? '');
	const importPercent = $derived(
		importProgress && importProgress.totalBytes > 0 ? Math.round((importProgress.bytesDone / importProgress.totalBytes) * 100) : 0
	);

	const onImportFile: FileDropZoneProps['onUpload'] = async (files) => {
		importFile = files[0] ?? importFile;
	};

	const onImportFileRejected: FileDropZoneProps['onFileRejected'] = async ({ reason, file }) => {
		toast.error(m.project_import_failed(), { description: `${file.name}: ${reason}` });
	};

	function resetImport() {
		importFile = null;
		importNameOverride = null;
		importStart = false;
		importProgress = null;
		pickedImportEnvironmentId = '';
		importOpen = false;
	}

	async function handleImport() {
		if (!importFile || !importName.trim() || importing) return;
		const requestedEnvId = importEnvironmentId;
		importing = true;
		const result = await tryCatch(
			projectService.importProject(
				importFile,
				{ name: importName.trim(), start: importStart && canStartImport },
				requestedEnvId,
				(progress) => (importProgress = progress)
			)
		);
		await handleApiResultWithCallbacks({
			result,
			message: m.project_import_failed(),
			setLoadingState: (value) => (importing = value),
			onSuccess: async (project) => {
				toast.success(m.project_imported({ name: project.name }));
				resetImport();
				await Promise.all(
					listScopes([requestedEnvId]).map((listScope) =>
						queryClient.invalidateQueries({ queryKey: queryKeys.projects.listPrefix(listScope) })
					)
				);
			},
			onError: () => {
				importProgress = null;
			}
		});
	}

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canCreateProject) {
			buttons.push({
				id: 'create',
				action: 'create',
				placement: 'primary',
				label: m.compose_create_project(),
				onclick: () => goto(resolve('/(app)/projects/new'))
			});
			buttons.push({
				id: 'import',
				action: 'base',
				placement: 'secondary',
				label: m.import(),
				icon: UploadIcon,
				onclick: () => (importOpen = true)
			});
		}
		if (canDeployProject) {
			buttons.push({
				id: 'check-updates',
				action: 'update',
				label: m.compose_update_projects(),
				onclick: handleCheckForUpdates,
				loading: checkUpdatesMutation.isPending,
				disabled: !resourcesReady || checkUpdatesMutation.isPending
			});
		}
		buttons.push(pinViewAction('projects', projectRequestOptions, readableEnvironments('projects:list')));
		buttons.push({
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refreshCompose,
			loading: isManualRefreshing,
			disabled: isRefreshBlocked
		});
		return buttons;
	});

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.common_total(),
			value: totalCompose,
			icon: ProjectsIcon,
			iconColor: 'text-warning'
		},
		{
			title: m.common_running(),
			value: runningCompose,
			icon: PlayIcon,
			iconColor: 'text-success'
		},
		{
			title: m.common_stopped(),
			value: stoppedCompose,
			icon: StopIcon,
			iconColor: 'text-destructive'
		},
		{
			title: m.projects_archived_badge(),
			value: archivedCompose,
			icon: ContainersIcon,
			iconColor: 'text-muted-foreground'
		}
	]);
</script>

{#snippet projectsEmptyState()}
	<EmptyState icon={ProjectsIcon} title={m.projects_empty_title()} description={m.projects_empty_description()} />
{/snippet}

<ResponsiveDialog
	open={importOpen}
	onOpenChange={(next) => {
		if (!next && !importing) resetImport();
	}}
	title={m.import_project()}
	description={m.import_project_description()}
	contentClass="sm:max-w-xl"
	dismissible={!importing}
>
	<div class="grid min-w-0 gap-y-4 py-4" data-testid="project-import-dialog">
		<EnvironmentDestinationSelect
			bind:value={() => importEnvironmentId, (value) => (pickedImportEnvironmentId = value)}
			permission="projects:create"
			variant="field"
		/>
		{#if importFile}
			<div class="flex min-w-0 items-center justify-between gap-2 rounded-lg border border-border bg-muted/50 p-3">
				<div class="flex min-w-0 flex-col">
					<span class="text-sm font-medium break-all">{importFile.name}</span>
					<span class="text-xs text-muted-foreground">{displaySize(importFile.size)}</span>
				</div>
				<ArcaneButton
					action="base"
					tone="ghost"
					size="icon"
					onclick={() => (importFile = null)}
					disabled={importing}
					icon={CloseIcon}
				/>
			</div>
		{:else}
			<FileDropZone
				onUpload={onImportFile}
				onFileRejected={onImportFileRejected}
				accept=".tar.gz,.tgz"
				maxFiles={1}
				fileCount={0}
				disabled={importing}
			/>
		{/if}
		<div class="space-y-2">
			<Label for="import-project-name">{m.project_name()}</Label>
			<Input
				id="import-project-name"
				value={importName}
				autocomplete="off"
				disabled={importing}
				oninput={(event) => (importNameOverride = event.currentTarget.value)}
			/>
		</div>
		<LabeledSwitch
			id="import-start"
			checked={importStart && canStartImport}
			label={m.start_after_import()}
			disabled={!canStartImport || importing}
			onCheckedChange={(next) => (importStart = next)}
		/>
		{#if importing}
			<div class="flex flex-col gap-2" aria-live="polite">
				<div class="flex items-center gap-2 text-sm text-muted-foreground">
					<Spinner class="size-4" />
					<span>{importProgress && importPercent < 100 ? m.uploading() : m.importing_project()}</span>
					{#if importProgress && importPercent < 100}
						<span class="ml-auto tabular-nums">{importPercent}%</span>
					{/if}
				</div>
				{#if importProgress && importPercent < 100}
					<Progress value={importPercent} class="h-1.5" />
				{/if}
			</div>
		{/if}
	</div>

	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={resetImport} disabled={importing} />
		<ArcaneButton
			action="create"
			customLabel={m.import()}
			icon={UploadIcon}
			onclick={handleImport}
			loading={importing}
			disabled={!importFile || !importName.trim() || importing}
		/>
	{/snippet}
</ResponsiveDialog>

<ResourcePageLayout title={m.projects_title()} subtitle={m.compose_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		{#if projects}
			<ProjectsTable
				projects={withFailedEnvironments(projects, relatedFailures)}
				emptyState={projectsEmptyState}
				bind:selectedIds
				bind:requestOptions={
					() => projectRequestOptions, (options) => (baseProjectRequestOptions = withArchivedFilter(options, showArchived))
				}
				{showArchived}
				availableTags={projectTags}
				onToggleArchived={toggleArchived}
				onRetry={() => Promise.all([projectsQuery.refetch(), statusCountsQuery.refetch(), refetchFailedTags()])}
				refreshError={projectsQuery.error}
			/>
		{/if}
	{/snippet}
</ResourcePageLayout>
