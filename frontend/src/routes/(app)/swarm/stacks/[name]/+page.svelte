<script lang="ts">
	import { goto } from '$app/navigation';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { TabBar, type TabItem } from '$lib/components/tab-bar';
	import CodeEditor from '$lib/components/code-editor/editor.svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Tabs from '$lib/components/ui/tabs';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { LayersIcon, DockIcon, JobsIcon, TrashIcon, EditIcon, FileTextIcon, CodeIcon } from '$lib/icons';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { onMount } from 'svelte';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import SwarmServicesTable from '../../services/services-table.svelte';
	import SwarmTasksTable from '../../tasks/tasks-table.svelte';
	import type { SwarmStackSource } from '$lib/types/swarm.type';

	let { data } = $props();

	let stack = $state(untrack(() => data.stack));
	let services = $state(untrack(() => data.services));
	let tasks = $state(untrack(() => data.tasks));
	let source = $state<SwarmStackSource | null>(untrack(() => data.source));
	let sourceState = $state<'loading' | 'available' | 'missing' | 'forbidden' | 'error'>(untrack(() => data.sourceState));
	let servicesRequestOptions = $state(untrack(() => data.servicesRequestOptions));
	let tasksRequestOptions = $state(untrack(() => data.tasksRequestOptions));
	type StackTab = 'services' | 'tasks' | 'source';
	let selectedTab = $state<StackTab>(
		untrack(() => ((data.stack?.services ?? 0) > 0 ? 'services' : data.sourceState !== 'forbidden' ? 'source' : 'services'))
	);
	let isLoading = $state({ refresh: false, remove: false });

	const stackName = $derived(stack?.name ?? data.stackName);
	const hasLiveStack = $derived((stack?.services ?? 0) > 0);
	const canViewSource = $derived(sourceState !== 'forbidden');
	const tabItems = $derived<TabItem[]>([
		...(hasLiveStack ? [{ value: 'services', label: m.swarm_services_title(), icon: DockIcon }] : []),
		...(hasLiveStack ? [{ value: 'tasks', label: m.swarm_tasks_title(), icon: JobsIcon }] : []),
		...(canViewSource ? [{ value: 'source', label: 'Source', icon: FileTextIcon }] : [])
	]);
	const totalServices = $derived(services?.pagination?.totalItems ?? services?.data?.length ?? 0);
	const totalTasks = $derived(tasks?.pagination?.totalItems ?? tasks?.data?.length ?? 0);
	const stackSubtitle = $derived(
		hasLiveStack ? m.swarm_stack_namespace({ namespace: stack?.namespace ?? stackName }) : `Saved source for ${stackName}`
	);

	async function fetchStackServices(options: typeof servicesRequestOptions) {
		return swarmService.getStackServices(stackName, options);
	}

	async function fetchStackTasks(options: typeof tasksRequestOptions) {
		return swarmService.getStackTasks(stackName, options);
	}

	async function refreshSource(showErrorToast = false) {
		try {
			source = await swarmService.getStackSource(stackName);
			sourceState = 'available';
		} catch (err: any) {
			if (err?.status === 404) {
				source = null;
				sourceState = 'missing';
				return;
			}
			if (err?.status === 403) {
				source = null;
				sourceState = 'forbidden';
				return;
			}

			source = null;
			sourceState = 'error';
			if (showErrorToast) {
				toast.error(m.common_refresh_failed({ resource: `saved source (${stackName})` }));
			}
		}
	}

	async function refresh() {
		isLoading.refresh = true;
		try {
			const [stackResult, servicesResult, tasksResult] = await Promise.allSettled([
				swarmService.getStack(stackName),
				swarmService.getStackServices(stackName, servicesRequestOptions),
				swarmService.getStackTasks(stackName, tasksRequestOptions)
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
			await refreshSource(true);
		} finally {
			isLoading.refresh = false;
		}
	}

	useEnvironmentRefresh(refresh);

	onMount(() => {
		void refreshSource();
	});

	$effect(() => {
		const validTabs = tabItems.map((item) => item.value as StackTab);
		if (validTabs.length > 0 && !validTabs.includes(selectedTab)) {
			selectedTab = validTabs[0];
		}
	});

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

<ResourcePageLayout title={stackName} subtitle={stackSubtitle} icon={LayersIcon} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="flex min-h-[calc(100vh-18rem)] flex-col gap-4">
			{#if !hasLiveStack && canViewSource}
				<Card.Root variant="subtle">
					<Card.Content class="p-4 text-sm">
						This stack is not currently deployed, but Arcane still found its saved source files.
					</Card.Content>
				</Card.Root>
			{/if}

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
				<Tabs.Content value="source" class="flex min-h-0 flex-1 flex-col">
					{#if sourceState === 'available' && source}
						<div class="grid min-h-0 flex-1 auto-rows-fr gap-6 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
							<Card.Root class="flex min-h-0 min-w-0 flex-col overflow-hidden">
								<Card.Header icon={CodeIcon} class="flex-shrink-0">
									<Card.Title>
										<h2>compose.yaml</h2>
									</Card.Title>
								</Card.Header>
								<Card.Content class="relative z-0 flex min-h-0 flex-1 flex-col overflow-visible p-0">
									<div class="absolute inset-0 min-h-0 w-full min-w-0 rounded-b-xl">
										<CodeEditor
											value={source.composeContent}
											language="yaml"
											readOnly={true}
											fontSize="13px"
											fileId={`swarm-stack-source:${stackName}:compose.yaml`}
										/>
									</div>
								</Card.Content>
							</Card.Root>

							<Card.Root class="flex min-h-0 min-w-0 flex-col overflow-hidden">
								<Card.Header icon={FileTextIcon} class="flex-shrink-0">
									<Card.Title>
										<h2>.env</h2>
									</Card.Title>
								</Card.Header>
								<Card.Content class="relative z-0 flex min-h-0 flex-1 flex-col overflow-visible p-0">
									{#if source.envContent?.trim()}
										<div class="absolute inset-0 min-h-0 w-full min-w-0 rounded-b-xl">
											<CodeEditor
												value={source.envContent}
												language="env"
												readOnly={true}
												fontSize="13px"
												fileId={`swarm-stack-source:${stackName}:.env`}
											/>
										</div>
									{:else}
										<div class="text-muted-foreground flex flex-1 items-center justify-center p-6 text-center text-sm">
											No saved `.env` file was stored for this stack.
										</div>
									{/if}
								</Card.Content>
							</Card.Root>
						</div>
					{:else if sourceState === 'loading'}
						<Card.Root variant="subtle">
							<Card.Content class="text-muted-foreground p-6 text-center text-sm">Loading saved source...</Card.Content>
						</Card.Root>
					{:else if sourceState === 'missing'}
						<Card.Root variant="subtle">
							<Card.Content class="p-6 text-sm">
								<div class="space-y-2">
									<p class="font-medium">{m.common_not_found_title({ resource: 'Saved source' })}</p>
									<p class="text-muted-foreground">
										{m.common_not_found_description({ resource: 'saved source' })}
									</p>
								</div>
							</Card.Content>
						</Card.Root>
					{:else if sourceState === 'error'}
						<Card.Root variant="subtle">
							<Card.Content class="p-6 text-sm">
								<div class="space-y-2">
									<p class="font-medium">{m.common_action_failed()}</p>
									<p class="text-muted-foreground">Arcane couldn’t load the saved source files for this stack.</p>
								</div>
							</Card.Content>
						</Card.Root>
					{/if}
				</Tabs.Content>
			</Tabs.Root>
		</div>
	{/snippet}
</ResourcePageLayout>
