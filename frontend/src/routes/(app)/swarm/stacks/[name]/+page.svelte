<script lang="ts">
	import { goto } from '$app/navigation';
	import { TabBar, type TabItem } from '$lib/components/tab-bar';
	import * as Card from '$lib/components/ui/card';
	import * as Tabs from '$lib/components/ui/tabs';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { LayersIcon, DockIcon, JobsIcon, EditIcon } from '$lib/icons';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import SwarmServicesTable from '../../services/services-table.svelte';
	import SwarmTasksTable from '../../tasks/tasks-table.svelte';

	let { data } = $props();

	let stack = $state(untrack(() => data.stack));
	let services = $state(untrack(() => data.services));
	let tasks = $state(untrack(() => data.tasks));
	let stackProject = $state(untrack(() => data.stackProject));
	let stackProjectState = $state<'available' | 'missing' | 'error'>(untrack(() => data.stackProjectState));
	let servicesRequestOptions = $state(untrack(() => data.servicesRequestOptions));
	let tasksRequestOptions = $state(untrack(() => data.tasksRequestOptions));
	type StackTab = 'services' | 'tasks';
	let selectedTab = $state<StackTab>('services');
	let isLoading = $state({ refresh: false, down: false });

	const stackName = $derived(stack?.name ?? data.stackName);
	const tabItems = $derived<TabItem[]>([
		{ value: 'services', label: m.swarm_services_title(), icon: DockIcon },
		{ value: 'tasks', label: m.swarm_tasks_title(), icon: JobsIcon }
	]);
	const totalServices = $derived(services?.pagination?.totalItems ?? services?.data?.length ?? 0);
	const totalTasks = $derived(tasks?.pagination?.totalItems ?? tasks?.data?.length ?? 0);
	const stackSubtitle = $derived(m.swarm_stack_namespace({ namespace: stack?.namespace ?? stackName }));
	const savedFilesHref = $derived(`/projects/swarm/${encodeURIComponent(stackName)}`);
	const hasSavedFiles = $derived(stackProjectState === 'available' && !!stackProject);

	async function fetchStackServices(options: typeof servicesRequestOptions) {
		return swarmService.getStackServices(stackName, options);
	}

	async function fetchStackTasks(options: typeof tasksRequestOptions) {
		return swarmService.getStackTasks(stackName, options);
	}

	async function refresh() {
		isLoading.refresh = true;
		try {
			const [stackResult, servicesResult, tasksResult, stackProjectResult] = await Promise.allSettled([
				swarmService.getStack(stackName),
				swarmService.getStackServices(stackName, servicesRequestOptions),
				swarmService.getStackTasks(stackName, tasksRequestOptions),
				swarmService.getStackProject(stackName)
			]);

			if (stackResult.status === 'fulfilled') {
				stack = stackResult.value;
			} else {
				toast.error(m.common_refresh_failed({ resource: `${m.swarm_stack()} "${stackName}"` }));
			}

			if (servicesResult.status === 'fulfilled') {
				services = servicesResult.value;
			} else {
				toast.error(m.common_refresh_failed({ resource: `${m.swarm_services_title()} (${stackName})` }));
			}

			if (tasksResult.status === 'fulfilled') {
				tasks = tasksResult.value;
			} else {
				toast.error(m.common_refresh_failed({ resource: `${m.swarm_tasks_title()} (${stackName})` }));
			}

			if (stackProjectResult.status === 'fulfilled') {
				stackProject = stackProjectResult.value;
				stackProjectState = 'available';
			} else if ((stackProjectResult.reason as { status?: number } | undefined)?.status === 404) {
				stackProject = null;
				stackProjectState = 'missing';
			} else {
				stackProject = null;
				stackProjectState = 'error';
			}
		} finally {
			isLoading.refresh = false;
		}
	}

	useEnvironmentRefresh(refresh);

	$effect(() => {
		const validTabs = tabItems.map((item) => item.value as StackTab);
		if (validTabs.length > 0 && !validTabs.includes(selectedTab) && validTabs[0]) {
			selectedTab = validTabs[0];
		}
	});

	function handleDown() {
		openConfirmDialog({
			title: `${m.common_down()} ${m.swarm_stack()}`,
			message: `Bring down the live runtime for "${stackName}" and keep any saved files?`,
			confirm: {
				label: m.common_down(),
				destructive: true,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.downStack(stackName)),
						message: m.common_action_failed(),
						setLoadingState: (v) => (isLoading.down = v),
						onSuccess: async () => {
							toast.success(`${m.swarm_stack()} "${stackName}" was brought down.`);
							goto(`/projects/swarm/${encodeURIComponent(stackName)}`, { invalidateAll: true });
						}
					});
				}
			}
		});
	}

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'saved-files',
			action: 'base',
			label: hasSavedFiles ? 'Open saved files' : 'Create saved files',
			icon: EditIcon,
			onclick: () => goto(savedFilesHref),
			disabled: isLoading.down
		},
		{
			id: 'down',
			action: 'stop',
			label: m.common_down(),
			onclick: handleDown,
			loading: isLoading.down,
			disabled: isLoading.down
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.swarm_services_title(),
			value: totalServices,
			icon: DockIcon,
			iconColor: 'text-blue-500'
		},
		{
			title: m.swarm_tasks_title(),
			value: totalTasks,
			icon: JobsIcon,
			iconColor: 'text-indigo-500'
		}
	]);
</script>

<ResourcePageLayout title={stackName} subtitle={stackSubtitle} icon={LayersIcon} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="flex min-h-[calc(100vh-18rem)] flex-col gap-4">
			<Card.Root variant="subtle">
				<Card.Content class="space-y-2 p-4 text-sm">
					{#if hasSavedFiles}
						<p class="font-medium">Saved stack files are managed in Projects.</p>
						<p class="text-muted-foreground">
							Use the button above to edit the saved compose files separately from this live runtime view.
						</p>
					{:else if stackProjectState === 'missing'}
						<p class="font-medium">No saved files exist for this live stack yet.</p>
						<p class="text-muted-foreground">
							Use Create saved files to start managing compose files for "{stackName}" in Projects.
						</p>
					{:else}
						<p class="font-medium">Arcane could not confirm whether saved files exist for this stack.</p>
						<p class="text-muted-foreground">You can still open the Projects route to create or inspect saved files.</p>
					{/if}
				</Card.Content>
			</Card.Root>

			<Tabs.Root value={selectedTab} class="flex min-h-0 flex-1 flex-col">
				<div class="w-fit pb-3">
					<TabBar items={tabItems} value={selectedTab} onValueChange={(value) => (selectedTab = value as StackTab)} />
				</div>

				<Tabs.Content value="services" class="min-h-0 flex-1">
					<SwarmServicesTable
						bind:services
						bind:requestOptions={servicesRequestOptions}
						fetchServices={fetchStackServices}
						persistKey={`arcane-swarm-stack-services-table-${stackName}`}
					/>
				</Tabs.Content>
				<Tabs.Content value="tasks" class="min-h-0 flex-1">
					<SwarmTasksTable
						bind:tasks
						bind:requestOptions={tasksRequestOptions}
						fetchTasks={fetchStackTasks}
						persistKey={`arcane-swarm-stack-tasks-table-${stackName}`}
					/>
				</Tabs.Content>
			</Tabs.Root>
		</div>
	{/snippet}
</ResourcePageLayout>
