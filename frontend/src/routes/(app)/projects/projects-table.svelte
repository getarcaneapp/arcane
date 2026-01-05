<script lang="ts">
	import type { Project } from '$lib/types/project.type';
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { EllipsisIcon, EditIcon, StartIcon, RestartIcon, StopIcon, TrashIcon } from '$lib/icons';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import { getStatusVariant } from '$lib/utils/status.utils';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';
	import { format } from 'date-fns';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { projectService } from '$lib/services/project-service';
	import { FolderOpenIcon, LayersIcon, CalendarIcon } from '$lib/icons';

	let {
		projects = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable()
	}: {
		projects: Paginated<Project>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	let isLoading = $state({
		start: false,
		stop: false,
		restart: false,
		remove: false,
		destroy: false,
		pull: false,
		updating: false
	});

	function getStatusTooltip(project: Project): string | undefined {
		return project.status.toLowerCase() === 'unknown' && project.statusReason ? project.statusReason : undefined;
	}

	async function performProjectAction(action: string, id: string) {
		isLoading[action as keyof typeof isLoading] = true;

		try {
			if (action === 'start') {
				handleApiResultWithCallbacks({
					result: await tryCatch(projectService.deployProject(id)),
					message: m.compose_start_failed(),
					setLoadingState: (value) => (isLoading.start = value),
					onSuccess: async () => {
						toast.success(m.compose_start_success());
						projects = await projectService.getProjects(requestOptions);
					}
				});
			} else if (action === 'stop') {
				handleApiResultWithCallbacks({
					result: await tryCatch(projectService.downProject(id)),
					message: m.compose_stop_failed(),
					setLoadingState: (value) => (isLoading.stop = value),
					onSuccess: async () => {
						toast.success(m.compose_stop_success());
						projects = await projectService.getProjects(requestOptions);
					}
				});
			} else if (action === 'restart') {
				handleApiResultWithCallbacks({
					result: await tryCatch(projectService.restartProject(id)),
					message: m.compose_restart_failed(),
					setLoadingState: (value) => (isLoading.restart = value),
					onSuccess: async () => {
						toast.success(m.compose_restart_success());
						projects = await projectService.getProjects(requestOptions);
					}
				});
			} else if (action === 'pull') {
				handleApiResultWithCallbacks({
					result: await tryCatch(projectService.pullProjectImages(id)),
					message: m.compose_pull_failed(),
					setLoadingState: (value) => (isLoading.pull = value),
					onSuccess: async () => {
						toast.success(m.compose_pull_success());
						projects = await projectService.getProjects(requestOptions);
					}
				});
			} else if (action === 'destroy') {
				openConfirmDialog({
					title: m.common_confirm_removal_title(),
					message: m.compose_confirm_removal_message(),
					checkboxes: [
						{
							id: 'volumes',
							label: m.confirm_remove_volumes_warning(),
							initialState: false
						},
						{
							id: 'files',
							label: m.confirm_remove_project_files(),
							initialState: false
						}
					],
					confirm: {
						label: m.compose_destroy(),
						destructive: true,
						action: async (result: any) => {
							const removeVolumes = !!(result?.checkboxes?.volumes ?? result?.volumes);
							const removeFiles = !!(result?.checkboxes?.files ?? result?.files);

							handleApiResultWithCallbacks({
								result: await tryCatch(projectService.destroyProject(id, removeVolumes, removeFiles)),
								message: m.compose_destroy_failed(),
								setLoadingState: (value) => (isLoading.destroy = value),
								onSuccess: async () => {
									toast.success(m.compose_destroy_success());
									projects = await projectService.getProjects(requestOptions);
								}
							});
						}
					}
				});
			}
		} catch (error) {
			toast.error(m.common_action_failed());
		}
	}

	const isAnyLoading = $derived(Object.values(isLoading).some((loading) => loading));

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
		{ accessorKey: 'serviceCount', title: m.compose_services(), sortable: true }
	] satisfies ColumnSpec<Project>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'serviceCount', label: m.compose_services(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: Project })}
	<a class="font-medium hover:underline" href="/projects/{item.id}">
		{item.name}
	</a>
{/snippet}

{#snippet StatusCell({ item }: { item: Project })}
	<StatusBadge
		variant={getStatusVariant(item.status)}
		text={capitalizeFirstLetter(item.status)}
		tooltip={getStatusTooltip(item)}
	/>
{/snippet}

{#snippet CreatedCell({ value }: { value: unknown })}
	{#if value}{format(new Date(String(value)), 'PP p')}{/if}
{/snippet}

{#snippet ProjectMobileCardSnippet({
	row,
	item,
	mobileFieldVisibility
}: {
	row: any;
	item: Project;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={(item: Project) => ({
			component: FolderOpenIcon,
			variant: item.status === 'running' ? 'emerald' : item.status === 'exited' ? 'red' : 'amber'
		})}
		title={(item: Project) => item.name}
		subtitle={(item: Project) => ((mobileFieldVisibility.id ?? true) ? item.id : null)}
		badges={[
			(item: Project) =>
				(mobileFieldVisibility.status ?? true)
					? {
							variant: getStatusVariant(item.status),
							text: capitalizeFirstLetter(item.status),
							tooltip: getStatusTooltip(item)
						}
					: null
		]}
		fields={[
			{
				label: m.compose_services(),
				getValue: (item: Project) => {
					const serviceCount = item.serviceCount ? Number(item.serviceCount) : (item.services?.length ?? 0);
					return `${serviceCount} ${Number(serviceCount) === 1 ? 'service' : 'services'}`;
				},
				icon: LayersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.serviceCount ?? true
			}
		]}
		footer={(mobileFieldVisibility.createdAt ?? true) && item.createdAt
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
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<ArcaneButton
					{...props}
					action="base"
					tone="ghost"
					size="icon"
					class="relative size-8 p-0"
					icon={EllipsisIcon}
					showLabel={false}
					customLabel={m.common_open_menu()}
				/>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => goto(`/projects/${item.id}`)} disabled={isAnyLoading}>
					<EditIcon class="size-4" />
					{m.common_edit()}
				</DropdownMenu.Item>

				{#if item.status !== 'running'}
					<DropdownMenu.Item onclick={() => performProjectAction('start', item.id)} disabled={isLoading.start || isAnyLoading}>
						{#if isLoading.start}
							<Spinner class="size-4" />
						{:else}
							<StartIcon class="size-4" />
						{/if}
						{m.common_up()}
					</DropdownMenu.Item>
				{:else}
					<DropdownMenu.Item
						onclick={() => performProjectAction('restart', item.id)}
						disabled={isLoading.restart || isAnyLoading}
					>
						{#if isLoading.restart}
							<Spinner class="size-4" />
						{:else}
							<RestartIcon class="size-4" />
						{/if}
						{m.common_restart()}
					</DropdownMenu.Item>

					<DropdownMenu.Item onclick={() => performProjectAction('stop', item.id)} disabled={isLoading.stop || isAnyLoading}>
						{#if isLoading.stop}
							<Spinner class="size-4" />
						{:else}
							<StopIcon class="size-4" />
						{/if}
						{m.common_down()}
					</DropdownMenu.Item>
				{/if}

				<DropdownMenu.Item onclick={() => performProjectAction('pull', item.id)} disabled={isLoading.pull || isAnyLoading}>
					{#if isLoading.pull}
						<Spinner class="size-4" />
					{:else}
						<RestartIcon class="size-4" />
					{/if}
					{m.compose_pull_redeploy()}
				</DropdownMenu.Item>

				<DropdownMenu.Separator />

				<DropdownMenu.Item
					variant="destructive"
					onclick={() => performProjectAction('destroy', item.id)}
					disabled={isLoading.remove || isAnyLoading}
				>
					{#if isLoading.remove}
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
	onRefresh={async (options) => (projects = await projectService.getProjects(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={ProjectMobileCardSnippet}
/>
