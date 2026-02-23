<script lang="ts">
	import { goto } from '$app/navigation';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { TabBar, type TabItem } from '$lib/components/tab-bar';
	import * as Tabs from '$lib/components/ui/tabs';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { LayersIcon, DockIcon, JobsIcon, TrashIcon, EditIcon } from '$lib/icons';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { parallelRefresh } from '$lib/utils/refresh.util';
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
	let servicesRequestOptions = $state(untrack(() => data.servicesRequestOptions));
	let tasksRequestOptions = $state(untrack(() => data.tasksRequestOptions));
	let selectedTab = $state<'services' | 'tasks'>('services');
	let isLoading = $state({ refresh: false, remove: false });

	const stackName = $derived(stack?.name ?? data.stackName);
	const tabItems = $derived<TabItem[]>([
		{ value: 'services', label: m.swarm_services_title(), icon: DockIcon },
		{ value: 'tasks', label: m.swarm_tasks_title(), icon: JobsIcon }
	]);
	const totalServices = $derived(services?.pagination?.totalItems ?? services?.data?.length ?? 0);
	const totalTasks = $derived(tasks?.pagination?.totalItems ?? tasks?.data?.length ?? 0);

	async function fetchStackServices(options: typeof servicesRequestOptions) {
		return swarmService.getStackServices(stackName, options);
	}

	async function fetchStackTasks(options: typeof tasksRequestOptions) {
		return swarmService.getStackTasks(stackName, options);
	}

	async function refresh() {
		await parallelRefresh(
			{
				stack: {
					fetch: () => swarmService.getStack(stackName),
					onSuccess: (value) => {
						stack = value;
					},
					errorMessage: m.common_refresh_failed({ resource: `${m.swarm_stack()} "${stackName}"` })
				},
				services: {
					fetch: () => swarmService.getStackServices(stackName, servicesRequestOptions),
					onSuccess: (value) => {
						services = value;
					},
					errorMessage: m.common_refresh_failed({ resource: `${m.swarm_services_title()} (${stackName})` })
				},
				tasks: {
					fetch: () => swarmService.getStackTasks(stackName, tasksRequestOptions),
					onSuccess: (value) => {
						tasks = value;
					},
					errorMessage: m.common_refresh_failed({ resource: `${m.swarm_tasks_title()} (${stackName})` })
				}
			},
			(v) => (isLoading.refresh = v)
		);
	}

	useEnvironmentRefresh(refresh);

	function handleDelete() {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.swarm_stack() }),
			message: m.common_delete_confirm({ resource: m.swarm_stack() }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.removeStack(stackName)),
						message: m.common_delete_failed({ resource: `${m.swarm_stack()} "${stackName}"` }),
						setLoadingState: (v) => (isLoading.remove = v),
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.swarm_stack()} "${stackName}"` }));
							goto('/swarm/stacks');
						}
					});
				}
			}
		});
	}

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'edit',
			action: 'base',
			label: m.common_edit(),
			icon: EditIcon,
			onclick: () => goto(`/swarm/stacks/new?fromStack=${encodeURIComponent(stackName)}`),
			disabled: isLoading.remove
		},
		{
			id: 'remove',
			action: 'remove',
			label: m.common_delete(),
			icon: TrashIcon,
			onclick: handleDelete,
			loading: isLoading.remove,
			disabled: isLoading.remove
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

<ResourcePageLayout
	title={stackName}
	subtitle={m.swarm_stack_namespace({ namespace: stack?.namespace ?? stackName })}
	icon={LayersIcon}
	{actionButtons}
	{statCards}
>
	{#snippet mainContent()}
		<div class="space-y-4">
			<Tabs.Root value={selectedTab}>
				<div class="w-fit">
					<TabBar items={tabItems} value={selectedTab} onValueChange={(value) => (selectedTab = value as 'services' | 'tasks')} />
				</div>

				<Tabs.Content value="services">
					<SwarmServicesTable
						bind:services
						bind:requestOptions={servicesRequestOptions}
						fetchServices={fetchStackServices}
						persistKey={`arcane-swarm-stack-services-table-${stackName}`}
					/>
				</Tabs.Content>
				<Tabs.Content value="tasks">
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
