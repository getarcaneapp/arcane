<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import userStore from '#lib/stores/user-store.js';
	import { hasPermission } from '#lib/utils/auth.js';

	import * as Card from '#lib/components/ui/card/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import { getSwarmTaskStateVariant, sortSwarmTasks } from '#lib/utils/swarm-tasks.js';
	import { JobsIcon, ConnectionIcon } from '#lib/icons/index.js';

	let {
		serviceName,
		serviceId
	}: {
		serviceName: string;
		serviceId: string;
	} = $props();

	const tasksQuery = createQuery(() => {
		const environmentId = environmentStore.selected?.id;
		const requestedServiceId = serviceId;
		$userStore;
		return {
			queryKey: queryKeys.swarm.serviceTasks(environmentId ?? '', requestedServiceId),
			queryFn: async () => {
				await environmentStore.ready;
				return swarmService.getServiceTasks(requestedServiceId, { pagination: { page: 1, limit: 100 } });
			},
			enabled: !!environmentId && !!serviceName && !!requestedServiceId && hasPermission('swarm:read', environmentId)
		};
	});
	const tasks = $derived(sortSwarmTasks(tasksQuery.data?.data ?? []));
	const isLoading = $derived(tasksQuery.isFetching);
	const hasLoaded = $derived(tasksQuery.isFetched);
</script>

<Card.Root>
	<Card.Header icon={JobsIcon}>
		<div class="flex flex-1 items-center justify-between">
			<div class="flex flex-col gap-1.5">
				<Card.Title>
					<h2>{m.tasks()}</h2>
				</Card.Title>
				<Card.Description>
					{m.swarm_service_tasks_count({ count: tasks.length })}
				</Card.Description>
			</div>
			<ArcaneButton action="refresh" size="sm" onclick={() => tasksQuery.refetch()} disabled={isLoading}>
				{m.common_refresh()}
			</ArcaneButton>
		</div>
	</Card.Header>
	<Card.Content class="p-4">
		{#if isLoading && !hasLoaded}
			<div class="py-12 text-center text-sm text-muted-foreground">{m.swarm_service_tasks_loading()}</div>
		{:else if tasks.length === 0}
			<div class="rounded-lg border border-dashed py-12 text-center text-muted-foreground">
				<div class="mx-auto mb-4 flex size-16 items-center justify-center rounded-full bg-muted/30">
					<JobsIcon class="size-6 text-muted-foreground" />
				</div>
				<div class="text-sm">{m.swarm_service_tasks_empty()}</div>
			</div>
		{:else}
			<div class="grid grid-cols-1 gap-3 lg:grid-cols-2 xl:grid-cols-3">
				{#each tasks as task (task.id)}
					<Card.Root variant="subtle">
						<Card.Content class="p-4">
							<div class="mb-3 flex items-center justify-between border-b border-border pb-3">
								<div class="min-w-0 flex-1">
									<div class="truncate text-sm font-semibold text-foreground" title={task.name}>
										{task.name}
									</div>
									<div class="font-mono text-xs text-muted-foreground">{task.id.slice(0, 12)}</div>
								</div>
								<Badge variant={getSwarmTaskStateVariant(task.currentState)} minWidth="20">{task.currentState}</Badge>
							</div>
							<div class="grid grid-cols-2 gap-2">
								<div>
									<div class="mb-1 text-xs font-semibold text-muted-foreground">
										{m.swarm_node()}
									</div>
									<div class="flex items-center gap-1">
										<ConnectionIcon class="size-3 text-muted-foreground" />
										<span class="truncate text-sm text-foreground">{task.nodeName || m.common_na()}</span>
									</div>
								</div>
								<div>
									<div class="mb-1 text-xs font-semibold text-muted-foreground">
										{m.swarm_desired_state()}
									</div>
									<Badge variant={getSwarmTaskStateVariant(task.desiredState)} size="sm" minWidth="20">{task.desiredState}</Badge>
								</div>
								{#if task.error}
									<div class="col-span-2">
										<div class="mb-1 text-xs font-semibold text-muted-foreground">{m.common_error()}</div>
										<div class="text-sm break-all text-red-400">{task.error}</div>
									</div>
								{/if}
							</div>
						</Card.Content>
					</Card.Root>
				{/each}
			</div>
		{/if}
	</Card.Content>
</Card.Root>
