<script lang="ts">
	import { goto } from '$app/navigation';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import CodeEditor from '#lib/components/code-editor/editor.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import { LayersIcon, DockIcon, JobsIcon, TrashIcon, EditIcon, FileTextIcon } from '#lib/icons/index.js';
	import EditorTabStrip from '#lib/components/editor-tab-strip.svelte';
	import WorkspaceFileTreePanel from '#lib/components/workspace-file-tree-panel.svelte';
	import ResizableSplit from '#lib/components/resizable-split.svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/api.js';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import SwarmServicesTable from '../../services/services-table.svelte';
	import SwarmTasksTable from '../../tasks/tasks-table.svelte';
	import type { SwarmStackSource } from '#lib/types/swarm.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';

	let { data } = $props();

	let stack = $derived(data.stack);
	let services = $derived(data.services);
	let tasks = $derived(data.tasks);
	let source = $derived<SwarmStackSource | null>(data.source);
	let sourceState = $derived<'loading' | 'available' | 'missing' | 'forbidden' | 'error'>(data.sourceState);
	let servicesRequestOptions = $derived(data.servicesRequestOptions);
	let tasksRequestOptions = $derived(data.tasksRequestOptions);

	let selectedSourceFile = $state('compose');
	let openSourceTabs = $state<string[]>(['compose']);
	let sourceTreeWidth = $state<number | null>(null);
	const sourceOpenTabs = $derived(openSourceTabs.length > 0 ? openSourceTabs : ['compose']);
	const activeSourceTab = $derived(
		sourceOpenTabs.includes(selectedSourceFile) ? selectedSourceFile : (sourceOpenTabs[0] ?? 'compose')
	);
	const sourceTabs = $derived(
		sourceOpenTabs.map((key) => ({
			key,
			label: key === 'compose' ? 'compose.yaml' : '.env',
			title: key === 'compose' ? 'compose.yaml' : '.env',
			iconClass: key === 'compose' ? 'text-blue-500' : 'text-green-500',
			pending: false
		}))
	);
	const sourceWorkspaceLeadingRows = [
		{ key: 'compose', label: 'compose.yaml', iconClass: 'text-blue-500', locked: true },
		{ key: 'env', label: '.env', iconClass: 'text-green-500', locked: true }
	];

	function openSourceTab(key: string) {
		if (!openSourceTabs.includes(key)) {
			openSourceTabs = [...openSourceTabs, key];
		}
		selectedSourceFile = key;
	}

	function closeSourceTab(key: string) {
		const index = sourceOpenTabs.indexOf(key);
		const remaining = sourceOpenTabs.filter((tab) => tab !== key);
		openSourceTabs = openSourceTabs.filter((tab) => tab !== key);
		if (selectedSourceFile === key) {
			selectedSourceFile = remaining[Math.min(Math.max(index - 1, 0), remaining.length - 1)] ?? 'compose';
		}
	}
	type StackTab = 'services' | 'tasks' | 'source';
	let isLoading = $state({ refresh: false, remove: false });

	const stackName = $derived(stack?.name ?? data.stackName);
	const hasLiveStack = $derived((stack?.services ?? 0) > 0);
	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const canManageStacks = $derived(hasPermission('swarm:stacks', currentEnvId));
	const canViewSource = $derived(canManageStacks && sourceState !== 'forbidden');
	const tabItems = $derived<TabItem[]>([
		...(hasLiveStack ? [{ value: 'services', label: m.services(), icon: DockIcon }] : []),
		...(hasLiveStack ? [{ value: 'tasks', label: m.tasks(), icon: JobsIcon }] : []),
		...(canViewSource ? [{ value: 'source', label: 'Source', icon: FileTextIcon }] : [])
	]);
	const urlTab = useUrlTab<StackTab>({
		validTabs: () => tabItems.map((tab) => tab.value as StackTab),
		defaultTab: () => (hasLiveStack ? 'services' : canViewSource ? 'source' : 'services')
	});
	const selectedTab = $derived(urlTab.value);
	const totalServices = $derived(services?.pagination?.totalItems ?? services?.data?.length ?? 0);
	const totalTasks = $derived(tasks?.pagination?.totalItems ?? tasks?.data?.length ?? 0);
	const stackSubtitle = $derived(
		hasLiveStack
			? m.swarm_stack_namespace({ namespace: stack?.namespace ?? stackName })
			: m.swarm_stack_saved_source({ stackName })
	);

	async function fetchStackServices(options: typeof servicesRequestOptions) {
		return swarmService.getStackServices(stackName, options);
	}

	async function fetchStackTasks(options: typeof tasksRequestOptions) {
		return swarmService.getStackTasks(stackName, options);
	}

	async function refreshSource(showErrorToast = false) {
		if (!canManageStacks) {
			source = null;
			sourceState = 'forbidden';
			return;
		}
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
				toast.error(m.common_refresh_failed({ resource: `${m.services()} (${stackName})` }));
			}

			if (tasksResult.status === 'fulfilled') {
				tasks = tasksResult.value;
			} else {
				toast.error(m.common_refresh_failed({ resource: `${m.tasks()} (${stackName})` }));
			}
			if (canManageStacks) {
				await refreshSource(true);
			}
		} finally {
			isLoading.refresh = false;
		}
	}

	useEnvironmentRefresh(refresh);

	onMount(() => {
		if (canManageStacks) {
			void refreshSource();
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
		...(canManageStacks
			? [
					{
						id: 'edit',
						action: 'base' as const,
						label: m.common_edit(),
						icon: EditIcon,
						onclick: () => goto(`/swarm/stacks/new?fromStack=${encodeURIComponent(stackName)}`),
						disabled: isLoading.remove
					},
					{
						id: 'remove',
						action: 'remove' as const,
						label: m.common_delete(),
						icon: TrashIcon,
						onclick: handleDelete,
						loading: isLoading.remove,
						disabled: isLoading.remove
					}
				]
			: []),
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
			title: m.services(),
			value: totalServices,
			icon: DockIcon,
			iconColor: 'text-blue-500'
		},
		{
			title: m.tasks(),
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
						{m.swarm_stacks_not_deployed_files_found()}
					</Card.Content>
				</Card.Root>
			{/if}

			<Tabs.Root value={selectedTab} class="flex min-h-0 flex-1 flex-col">
				<div class="w-fit pb-3">
					<TabBar items={tabItems} value={selectedTab} onValueChange={urlTab.select} />
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
						{@const stackSource = source}
						<div class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-border bg-card">
							<ResizableSplit
								class="min-h-0 flex-1"
								variant="flush"
								firstClass="bg-muted/20 border-border flex min-h-0 flex-col border-b lg:border-r lg:border-b-0"
								secondClass="flex min-h-0 flex-col"
								bind:size={sourceTreeWidth}
								minSize={200}
								maxSize={480}
								minSecondSize={360}
								defaultRatio={0.2}
								stackBelow={1024}
								ariaLabel={m.compose_editor_resize_files_panel()}
								persistKey={`arcane.swarm.split:${stackName}:source`}
							>
								{#snippet first()}
									<WorkspaceFileTreePanel
										leadingRows={sourceWorkspaceLeadingRows}
										entries={[]}
										selectedFile={selectedSourceFile}
										onSelect={openSourceTab}
									/>
								{/snippet}

								{#snippet second()}
									<div class="flex h-full min-h-0 flex-1 flex-col">
										<EditorTabStrip
											tabs={sourceTabs}
											activeKey={activeSourceTab}
											onSelect={openSourceTab}
											onClose={closeSourceTab}
										/>
										<div class="relative min-h-0 flex-1">
											{#key activeSourceTab}
												{#if activeSourceTab === 'compose'}
													<div class="absolute inset-0 min-h-0 w-full min-w-0">
														<CodeEditor
															value={stackSource.composeContent}
															language="yaml"
															readOnly={true}
															fontSize="13px"
															fileId={`swarm-stack-source:${stackName}:compose.yaml`}
														/>
													</div>
												{:else if stackSource.envContent?.trim()}
													<div class="absolute inset-0 min-h-0 w-full min-w-0">
														<CodeEditor
															value={stackSource.envContent}
															language="env"
															readOnly={true}
															fontSize="13px"
															fileId={`swarm-stack-source:${stackName}:.env`}
														/>
													</div>
												{:else}
													<div class="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
														No saved `.env` file was stored for this stack.
													</div>
												{/if}
											{/key}
										</div>
									</div>
								{/snippet}
							</ResizableSplit>
						</div>
					{:else if sourceState === 'loading'}
						<Card.Root variant="subtle">
							<Card.Content class="p-6 text-center text-sm text-muted-foreground">{m.swarm_stack_source_loading()}</Card.Content>
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
									<p class="text-muted-foreground">{m.swarm_stack_source_load_error()}</p>
								</div>
							</Card.Content>
						</Card.Root>
					{/if}
				</Tabs.Content>
			</Tabs.Root>
		</div>
	{/snippet}
</ResourcePageLayout>
