<script lang="ts">
	import { ProjectsIcon, StartIcon, StopIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import ProjectsTable from './projects-table.svelte';
	import { goto } from '$app/navigation';
	import { m } from '$lib/paraglide/messages';
	import { projectService } from '$lib/services/project-service';
	import { imageService } from '$lib/services/image-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';

	let { data } = $props();

	let projects = $state(untrack(() => data.projects));
	let projectStatusCounts = $state(untrack(() => data.projectStatusCounts));
	let projectRequestOptions = $state(untrack(() => data.projectRequestOptions));
	let selectedIds = $state<string[]>([]);

	let isLoading = $state({
		updating: false,
		refreshing: false
	});

	const totalCompose = $derived(projectStatusCounts.totalProjects);
	const runningCompose = $derived(projectStatusCounts.runningProjects);
	const stoppedCompose = $derived(projectStatusCounts.stoppedProjects);

	async function handleCheckForUpdates() {
		isLoading.updating = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(imageService.runAutoUpdate()),
			message: m.containers_check_updates_failed(),
			setLoadingState: (value) => (isLoading.updating = value),
			async onSuccess() {
				toast.success(m.compose_update_success());
				projects = await projectService.getProjects(projectRequestOptions);
			}
		});
	}

	async function refreshCompose() {
		isLoading.refreshing = true;
		let refreshingProjectList = true;
		let refreshingProjectCounts = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(projectService.getProjects(projectRequestOptions)),
			message: m.common_refresh_failed({ resource: m.projects_title() }),
			setLoadingState: (v) => {
				refreshingProjectList = v;
				isLoading.refreshing = refreshingProjectCounts || refreshingProjectList;
			},
			async onSuccess(newProjects) {
				projects = newProjects;
			}
		});
		handleApiResultWithCallbacks({
			result: await tryCatch(projectService.getProjectStatusCounts()),
			message: m.common_refresh_failed({ resource: m.projects_title() }),
			setLoadingState: (v) => {
				refreshingProjectCounts = v;
				isLoading.refreshing = refreshingProjectCounts || refreshingProjectList;
			},
			async onSuccess(newProjectCounts) {
				projectStatusCounts = newProjectCounts;
			}
		});
	}

	let lastEnvId: string | null = null;
	$effect(() => {
		const env = environmentStore.selected;
		if (!env) return;
		if (lastEnvId === null) {
			lastEnvId = env.id;
			return;
		}
		if (env.id !== lastEnvId) {
			lastEnvId = env.id;
			refreshCompose();
		}
	});

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.compose_create_project(),
			onclick: () => goto('/projects/new')
		},
		{
			id: 'check-updates',
			action: 'update',
			label: m.compose_update_projects(),
			onclick: handleCheckForUpdates,
			loading: isLoading.updating,
			disabled: isLoading.updating
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refreshCompose,
			loading: isLoading.refreshing,
			disabled: isLoading.refreshing
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.compose_total(),
			value: totalCompose,
			icon: ProjectsIcon,
			iconColor: 'text-amber-500'
		},
		{
			title: m.common_running(),
			value: runningCompose,
			icon: StartIcon,
			iconColor: 'text-green-500'
		},
		{
			title: m.common_stopped(),
			value: stoppedCompose,
			icon: StopIcon,
			iconColor: 'text-red-500'
		}
	]);
</script>

<ResourcePageLayout title={m.projects_title()} subtitle={m.compose_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<ProjectsTable bind:projects bind:selectedIds bind:requestOptions={projectRequestOptions} />
	{/snippet}
</ResourcePageLayout>
