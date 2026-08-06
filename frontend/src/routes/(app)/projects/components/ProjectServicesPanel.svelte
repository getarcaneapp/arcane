<script lang="ts">
	import { goto } from '$app/navigation';
	import { Badge } from '#lib/components/ui/badge';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import ContainerActionMenuItem from '#lib/components/arcane-table/cells/container-action-menu-item.svelte';
	import IconImage from '#lib/components/icon-image.svelte';
	import { PortBadge } from '#lib/components/badges/index.js';
	import { mode } from 'mode-watcher';
	import { getThemedIconUrl } from '#lib/utils/docker';
	import type { RuntimeService } from '#lib/types/swarm';
	import { m } from '#lib/paraglide/messages';
	import { projectService } from '#lib/services/project-service';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import { hasPermission } from '#lib/utils/auth';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks, tryCatch } from '#lib/utils/api';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast';
	import { confirmAndRemoveContainer, runContainerLifecycleAction } from '#lib/utils/container-actions';
	import { cn } from '#lib/utils';
	import { StartIcon, StopIcon, RefreshIcon, TrashIcon, InspectIcon, LayersIcon, BoxIcon, HealthIcon } from '#lib/icons';

	interface Props {
		services?: RuntimeService[];
		projectId?: string;
		onRefresh?: () => Promise<void>;
	}

	let { services = [], projectId, onRefresh }: Props = $props();

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const canStartContainer = $derived(hasPermission('containers:start', currentEnvId));
	const canStopContainer = $derived(hasPermission('containers:stop', currentEnvId));
	const canRestartProject = $derived(hasPermission('projects:restart', currentEnvId));
	const canDeleteContainer = $derived(hasPermission('containers:delete', currentEnvId));

	type ServiceWithId = RuntimeService & { id: string };

	const servicesWithIds = $derived<ServiceWithId[]>(
		(services ?? []).map((service) => ({
			...service,
			id: service.containerId || service.name
		}))
	);

	type ActionStatus = 'starting' | 'stopping' | 'restarting' | 'pausing' | 'unpausing' | 'removing' | '';
	let actionStatus = $state<Record<string, ActionStatus>>({});
	const isAnyLoading = $derived(Object.values(actionStatus).some(Boolean));

	function statusDotClass(status: string | undefined): string {
		const normalized = (status ?? '').toLowerCase();
		if (normalized === 'running') return 'bg-emerald-500';
		if (normalized === 'exited') return 'bg-red-500';
		return 'bg-amber-500';
	}

	function healthClass(health: string | undefined): string {
		const normalized = (health ?? '').toLowerCase();
		if (normalized === 'healthy') return 'text-emerald-500';
		if (normalized === 'unhealthy') return 'text-red-500';
		return 'text-amber-500';
	}

	// Runtime ports arrive as "8081:80/tcp" (or "80/tcp" when unpublished).
	function parseRuntimePorts(ports: string[]) {
		return ports.map((port) => {
			const [numsPart, proto] = port.split('/');
			const nums = (numsPart ?? '').split(':');
			if (nums.length === 2) {
				return { publicPort: parseInt(nums[0] ?? ''), privatePort: parseInt(nums[1] ?? ''), type: proto || 'tcp' };
			}
			return { privatePort: parseInt(nums[0] ?? ''), type: proto || 'tcp' };
		});
	}

	function getContainerUrl(service: RuntimeService): string {
		if (!service.containerId) return '#';
		return projectId
			? `/containers/${service.containerId}?from=project&projectId=${projectId}`
			: `/containers/${service.containerId}`;
	}

	async function performContainerAction(action: 'start' | 'stop' | 'restart', id: string) {
		await runContainerLifecycleAction({
			action,
			containerId: id,
			setStatus: (status) => {
				actionStatus[id] = status;
			},
			onRefresh
		});
	}

	async function performServiceRestart(item: ServiceWithId) {
		if (!projectId || !item.name) {
			toast.error(m.containers_restart_failed());
			return;
		}

		const id = item.id;
		actionStatus[id] = 'restarting';

		try {
			handleApiResultWithCallbacks({
				result: await tryCatch(projectService.restartProject(projectId, [item.name])),
				message: m.containers_restart_failed(),
				setLoadingState: (value) => {
					actionStatus[id] = value ? 'restarting' : '';
				},
				async onSuccess(data) {
					toast.success(m.containers_restart_success(), activityToastOptions(extractActivityId(data)));
					await onRefresh?.();
				}
			});
		} catch (error) {
			console.error('Service restart failed:', error);
			toast.error(m.containers_action_error());
			actionStatus[id] = '';
		}
	}

	function handleRemoveContainer(id: string, name: string) {
		confirmAndRemoveContainer({
			containerId: id,
			containerName: name,
			setStatus: (status) => {
				actionStatus[id] = status;
			},
			onRefresh
		});
	}
</script>

<div class="flex h-full min-h-0 flex-col">
	<div class="flex shrink-0 items-center gap-2 border-b border-border px-3 py-2">
		<LayersIcon class="size-4 text-muted-foreground" />
		<span class="text-sm font-medium">{m.services()}</span>
		<Badge variant="gray" size="sm">{servicesWithIds.length}</Badge>
	</div>
	<div class="min-h-0 flex-1 space-y-0.5 overflow-y-auto p-1.5">
		{#each servicesWithIds as item (item.id)}
			{@const status = actionStatus[item.id]}
			{@const running = item.status?.toLowerCase() === 'running'}
			<div class="group rounded-md px-2 py-1.5 transition-colors hover:bg-muted/60">
				<div class="flex items-center gap-2">
					<span class={cn('size-2 shrink-0 rounded-full', statusDotClass(item.status))} title={item.status}></span>
					<IconImage
						src={getThemedIconUrl(item, mode.current)}
						alt={item.name}
						fallback={BoxIcon}
						class="size-4"
						containerClass="size-5 bg-transparent ring-0"
					/>
					{#if item.containerId}
						<button
							type="button"
							class="min-w-0 flex-1 truncate text-left text-sm hover:underline"
							title={item.containerName || item.name}
							onclick={() => goto(getContainerUrl(item))}
						>
							{item.name}
						</button>
					{:else}
						<span class="min-w-0 flex-1 truncate text-sm text-muted-foreground" title={m.compose_service_not_created()}>
							{item.name}
						</span>
					{/if}
					{#if item.health}
						<HealthIcon class={cn('size-3.5 shrink-0', healthClass(item.health))} />
					{/if}
					{#if item.containerId}
						<RowActionsMenu triggerClass="size-6 shrink-0 opacity-0 group-hover:opacity-100 data-[state=open]:opacity-100">
							<DropdownMenu.Item onclick={() => goto(getContainerUrl(item))} disabled={isAnyLoading}>
								<InspectIcon class="size-4" />
								{m.common_inspect()}
							</DropdownMenu.Item>
							<DropdownMenu.Separator />
							{#if running}
								{#if canStopContainer}
									<ContainerActionMenuItem
										icon={StopIcon}
										label={m.common_stop()}
										onclick={() => performContainerAction('stop', item.containerId!)}
										loading={status === 'stopping'}
										disabled={status === 'stopping' || isAnyLoading}
									/>
								{/if}
								{#if canRestartProject}
									<ContainerActionMenuItem
										icon={RefreshIcon}
										label={m.common_restart()}
										onclick={() => performServiceRestart(item)}
										loading={status === 'restarting'}
										disabled={status === 'restarting' || isAnyLoading}
									/>
								{/if}
							{:else if canStartContainer}
								<ContainerActionMenuItem
									icon={StartIcon}
									label={m.common_start()}
									onclick={() => performContainerAction('start', item.containerId!)}
									loading={status === 'starting'}
									disabled={status === 'starting' || isAnyLoading}
								/>
							{/if}
							{#if canDeleteContainer}
								<DropdownMenu.Separator />
								<ContainerActionMenuItem
									icon={TrashIcon}
									label={m.common_remove()}
									onclick={() => handleRemoveContainer(item.containerId!, item.containerName || item.name)}
									loading={status === 'removing'}
									disabled={status === 'removing' || isAnyLoading}
									destructive
								/>
							{/if}
						</RowActionsMenu>
					{/if}
				</div>
				{#if item.serviceConfig?.ports?.length}
					<div class="mt-1 flex min-w-0 pl-7">
						<PortBadge ports={item.serviceConfig.ports} wrap={false} />
					</div>
				{:else if item.ports?.length}
					<div class="mt-1 flex min-w-0 pl-7">
						<PortBadge ports={parseRuntimePorts(item.ports)} wrap={false} />
					</div>
				{/if}
			</div>
		{/each}
		{#if servicesWithIds.length === 0}
			<p class="px-2 py-4 text-center text-sm text-muted-foreground">{m.compose_no_services_found()}</p>
		{/if}
	</div>
</div>
