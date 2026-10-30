<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createQuery } from '@tanstack/svelte-query';
	import { onMount, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import EditorTabStrip from '#lib/components/editor-tab-strip.svelte';
	import ResizableSplit from '#lib/components/resizable-split.svelte';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as CodeBlock from '#lib/components/ui/code-block/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import WorkspaceFileTreePanel from '#lib/components/workspace-file-tree-panel.svelte';
	import { useHighlightedCode } from '#lib/hooks/use-highlighted-code.svelte.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import { LayersIcon, DockIcon, JobsIcon, TrashIcon, EditIcon, FileTextIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { swarmStackQuery, swarmStackServiceListQuery, swarmStackTaskListQuery } from '#lib/query/swarm.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import type { SwarmStackSource } from '#lib/types/swarm.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SwarmServicesTable from '../../services/components/services-table.svelte';
	import SwarmTasksTable from '../../tasks/components/tasks-table.svelte';

	let { data } = $props();

	let source = $derived<SwarmStackSource | null>(data.source);
	let sourceState = $derived<'loading' | 'available' | 'missing' | 'forbidden' | 'error'>(data.sourceState);
	let servicesRequestOptions = $state(untrack(() => data.servicesRequestOptions));
	let tasksRequestOptions = $state(untrack(() => data.tasksRequestOptions));

	const stackQuery = createQuery(() => ({
		...swarmStackQuery(data.envId, data.stackName),
		placeholderData: (previous, query) => (query?.queryKey[1] === data.envId ? previous : undefined)
	}));
	const servicesQuery = createQuery(() => ({
		...swarmStackServiceListQuery(data.environment, data.stackName, servicesRequestOptions),
		placeholderData: (previous, query) => (query?.queryKey[1] === data.envId ? previous : undefined)
	}));
	const tasksQuery = createQuery(() => ({
		...swarmStackTaskListQuery(data.environment, data.stackName, tasksRequestOptions),
		placeholderData: (previous, query) => (query?.queryKey[1] === data.envId ? previous : undefined)
	}));
	const stack = $derived(stackQuery.data ?? data.stack);
	const services = $derived(servicesQuery.data ?? data.services);
	const tasks = $derived(tasksQuery.data ?? data.tasks);

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
			iconClass: key === 'compose' ? 'text-info' : 'text-success',
			pending: false
		}))
	);
	const sourceLines = useHighlightedCode(
		() => (activeSourceTab === 'compose' ? (source?.composeContent ?? '') : (source?.envContent ?? '')),
		() => (activeSourceTab === 'compose' ? 'yaml' : 'env')
	);
	const sourceWorkspaceLeadingRows = [
		{ key: 'compose', label: 'compose.yaml', iconClass: 'text-info', locked: true },
		{ key: 'env', label: '.env', iconClass: 'text-success', locked: true }
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
	const isLoading = $state({ refresh: false, remove: false });

	const stackName = $derived(stack?.name ?? data.stackName);
	const hasLiveStack = $derived((stack?.services ?? 0) > 0);
	const envId = $derived(data.envId);
	const canManageStacks = $derived(hasPermission('swarm:stacks', envId));
	const canViewSource = $derived(canManageStacks && sourceState !== 'forbidden');
	const tabItems = $derived<TabItem[]>([
		...(hasLiveStack ? [{ value: 'services', label: m.services(), icon: DockIcon }] : []),
		...(hasLiveStack ? [{ value: 'tasks', label: m.tasks(), icon: JobsIcon }] : []),
		...(canViewSource ? [{ value: 'source', label: 'Source', icon: FileTextIcon }] : [])
	]);
	const urlTab = useUrlTab<StackTab>({
		validTabs: () => tabItems.map((tab) => tab.value as StackTab),
		defaultTab: () => {
			if (hasLiveStack) return 'services';
			if (canViewSource) return 'source';
			return 'services';
		}
	});
	const selectedTab = $derived(urlTab.value);
	const totalServices = $derived(services?.pagination?.totalItems ?? services?.data?.length ?? 0);
	const totalTasks = $derived(tasks?.pagination?.totalItems ?? tasks?.data?.length ?? 0);
	const stackSubtitle = $derived(
		hasLiveStack
			? m.swarm_stack_namespace({ namespace: stack?.namespace ?? stackName })
			: m.swarm_stack_saved_source({ stackName })
	);

	async function refreshSource(showErrorToast = false) {
		if (!canManageStacks) {
			source = null;
			sourceState = 'forbidden';
			return;
		}
		const operationResult = await tryCatch(
			(async () => {
				source = await swarmService.getStackSource(stackName, envId);
				sourceState = 'available';
			})()
		);
		if (operationResult.error !== null) {
			const err = operationResult.error;

			if ('status' in err && err.status === 404) {
				source = null;
				sourceState = 'missing';
				return;
			}
			if ('status' in err && err.status === 403) {
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
			const [stackResult, servicesResult, tasksResult] = await Promise.all([
				stackQuery.refetch(),
				servicesQuery.refetch(),
				tasksQuery.refetch()
			]);

			if (stackResult.isError) {
				toast.error(m.common_refresh_failed({ resource: `${m.swarm_stack()} "${stackName}"` }));
			}

			if (servicesResult.isError) {
				toast.error(m.common_refresh_failed({ resource: `${m.services()} (${stackName})` }));
			}

			if (tasksResult.isError) {
				toast.error(m.common_refresh_failed({ resource: `${m.tasks()} (${stackName})` }));
			}
			if (canManageStacks) {
				await refreshSource(true);
			}
		} finally {
			isLoading.refresh = false;
		}
	}

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
					isLoading.remove = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.removeStack(stackName, envId)),
						message: m.common_delete_failed({ resource: `${m.swarm_stack()} "${stackName}"` }),
						setLoadingState: (v) => (isLoading.remove = v),
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.swarm_stack()} "${stackName}"` }));
							goto(resolve('/(app)/swarm/stacks'));
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
						placement: 'primary' as const,
						label: m.common_edit(),
						icon: EditIcon,
						onclick: () =>
							goto(
								resolve(`/(app)/swarm/stacks/new?fromStack=${encodeURIComponent(stackName)}&env=${encodeURIComponent(envId)}`)
							),
						disabled: isLoading.remove
					},
					{
						id: 'remove',
						action: 'remove' as const,
						group: 'danger' as const,
						destructive: true,
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
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
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
			iconColor: 'text-info'
		},
		{
			title: m.tasks(),
			value: totalTasks,
			icon: JobsIcon,
			iconColor: 'text-indigo'
		}
	]);
</script>

<ResourcePageLayout title={stackName} subtitle={stackSubtitle} icon={LayersIcon} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="flex min-h-screen-inset-18 flex-col gap-4">
			{#if !hasLiveStack && canViewSource}
				<Card.Root>
					<Card.Content>
						<p class="text-sm">{m.swarm_stacks_not_deployed_files_found()}</p>
					</Card.Content>
				</Card.Root>
			{/if}

			<Tabs.Root value={selectedTab} class="flex min-h-0 flex-1 flex-col">
				<div class="w-fit pb-3">
					<TabBar items={tabItems} value={selectedTab} onValueChange={urlTab.select} />
				</div>

				<Tabs.Content value="services" class="min-h-0 flex-1">
					<SwarmServicesTable
						{services}
						invalidateKey={queryKeys.swarm.stack(envId, data.stackName)}
						bind:requestOptions={servicesRequestOptions}
						onRetry={() => servicesQuery.refetch()}
						persistKey={`arcane-swarm-stack-services-table-${stackName}`}
						showEnvironment={false}
					/>
				</Tabs.Content>
				<Tabs.Content value="tasks" class="min-h-0 flex-1">
					<SwarmTasksTable
						{tasks}
						bind:requestOptions={tasksRequestOptions}
						onRetry={() => tasksQuery.refetch()}
						persistKey={`arcane-swarm-stack-tasks-table-${stackName}`}
						showEnvironment={false}
					/>
				</Tabs.Content>
				<Tabs.Content value="source" class="flex min-h-0 flex-1 flex-col">
					{#if sourceState === 'available' && source}
						{@const stackSource = source}
						<div class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-border bg-card">
							{#key `arcane.swarm.split:${stackName}:source`}
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
												{#if activeSourceTab === 'compose' || stackSource.envContent?.trim()}
													<CodeBlock.Root variant="ghost" class="absolute inset-0">
														<CodeBlock.Content lineNumbers class="flex-1">
															{#each sourceLines.current as line, index (index)}
																<CodeBlock.Line code={line} />
															{/each}
														</CodeBlock.Content>
													</CodeBlock.Root>
												{:else}
													<div class="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
														No saved `.env` file was stored for this stack.
													</div>
												{/if}
											</div>
										</div>
									{/snippet}
								</ResizableSplit>
							{/key}
						</div>
					{:else if sourceState === 'loading'}
						<Card.Root>
							<Card.Content>
								<p class="text-center text-sm text-muted-foreground">{m.swarm_stack_source_loading()}</p>
							</Card.Content>
						</Card.Root>
					{:else if sourceState === 'missing'}
						<Card.Root>
							<Card.Content>
								<div class="space-y-2 text-sm">
									<p class="font-medium">{m.common_not_found_title({ resource: 'Saved source' })}</p>
									<p class="text-muted-foreground">
										{m.common_not_found_description({ resource: 'saved source' })}
									</p>
								</div>
							</Card.Content>
						</Card.Root>
					{:else if sourceState === 'error'}
						<Card.Root>
							<Card.Content>
								<div class="space-y-2 text-sm">
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
